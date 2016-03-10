package localquota

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"

	"github.com/openshift/origin/pkg/volume/empty_dir"
	exutil "github.com/openshift/origin/test/extended/util"
)

const (
	volDirEnvVar       = "VOLUME_DIR"
	podCreationTimeout = 30     // seconds
	expectedQuotaKb    = 262144 // launcher script sets 256Mi, xfs_quota reports in Kb.
)

// setFSGroupMustRunAs flips the restricted SCC's FSGroup setting
// to MustRunAs. May not be required for long as this will soon be
// the default with: https://github.com/openshift/origin/pull/7334
func setFSGroupMustRunAs(oc *exutil.CLI) error {
	// Write out to temp file so we can edit the SCC and update it:
	tempFile, err := ioutil.TempFile("", "fsgroup-scc-edit")
	defer os.Remove(tempFile.Name())

	outBytes, err := oc.AsAdmin().Run("export").Args("scc/restricted").Output()
	scc := string(outBytes)
	// replace the fsGroup:
	scc = strings.Replace(scc, "fsGroup:\n  type: RunAsAny", "fsGroup:\n  type: MustRunAs", -1)
	_, writeErr := tempFile.WriteString(string(scc))
	if writeErr != nil {
		return writeErr
	}

	outBytes, err = oc.AsAdmin().Run("replace").Args("-f", tempFile.Name()).Output()
	if err != nil {
		return err
	}
	return nil
}

func lookupFSGroup(oc *exutil.CLI, project string) (int, error) {
	gidRange, err := oc.Run("get").Args("project", project,
		"--template='{{ index .metadata.annotations \"openshift.io/sa.scc.supplemental-groups\" }}'").Output()
	if err != nil {
		return 0, err
	}

	// gidRange will be something like: 1000030000/10000
	fsGroupStr := strings.Split(gidRange, "/")[0]
	fsGroupStr = strings.Replace(fsGroupStr, "'", "", -1)

	fsGroup, err := strconv.Atoi(fsGroupStr)
	if err != nil {
		return 0, err
	}

	return fsGroup, nil
}

// lookupXFSQuota runs an xfs_quota report and parses the output
// looking for the given fsGroup ID's hard quota.
//
// Output from this command looks like:
//
// $ xfs_quota -x -c 'report -n  -L 1000030000 -U 1000030000' /tmp/openshift/xfs-vol-dir
// Group quota on /tmp/openshift/xfs-vol-dir (/dev/sdb2)
//                                Blocks
// Group ID         Used       Soft       Hard    Warn/Grace
// ---------- --------------------------------------------------
// #1000030000          0     524288     524288     00 [--------]
func lookupXFSQuota(oc *exutil.CLI, fsGroup int, volDir string) (int, error) {

	// First lookup the filesystem device the volumeDir resides on:
	outBytes, err := exec.Command("df", "--output=source", volDir).Output()
	if err != nil {
		return 0, err
	}
	fsDevice, parseErr := empty_dir.ParseFsDevice(string(outBytes))
	if parseErr != nil {
		return 0, parseErr
	}

	args := []string{"xfs_quota", "-x", "-c",
		fmt.Sprintf("report -n -L %d -U %d", fsGroup, fsGroup),
		fsDevice}
	cmd := exec.Command("sudo", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outBytes, err = cmd.Output()
	if err != nil {
		return 0, err
	}
	quotaReport := string(outBytes)

	// Parse output looking for lines starting with a #, which are the lines with
	// group IDs and their quotas:
	lines := strings.Split(quotaReport, "\n")
	var quota int
	foundFsGroup := false
	for _, l := range lines {
		if strings.HasPrefix(l, fmt.Sprintf("#%d", fsGroup)) {
			foundFsGroup = true
			words := strings.Fields(l)
			if len(words) != 6 {
				return 0, fmt.Errorf("expected 6 words in quota line: %s", l)
			}
			quota, err := strconv.Atoi(words[3])
			if err != nil {
				return 0, err
			}
			return quota, nil
		}
	}
	if !foundFsGroup {
		return 0, fmt.Errorf("unable to find any quota for group ID: %d", fsGroup)
	}

	return quota, nil
}

// waitForQuotaToBeApplied will check for the expected quota, and wait a short interval if
// not found until we reach the timeout. If we were unable to find the quota we expected,
// an error will be returned. If we found the expected quota in time we will return nil.
func waitForQuotaToBeApplied(oc *exutil.CLI, fsGroup int, volDir string) error {
	secondsWaited := 0
	for secondsWaited < podCreationTimeout {
		quotaFound, quotaErr := lookupXFSQuota(oc, fsGroup, volDir)
		o.Expect(quotaErr).NotTo(o.HaveOccurred())
		if quotaFound == expectedQuotaKb {
			return nil
		}

		time.Sleep(1 * time.Second)
		secondsWaited = secondsWaited + 1
	}

	return fmt.Errorf("expected quota was not applied in time")
}

var _ = g.Describe("[volumes] Test local storage quota", func() {
	defer g.GinkgoRecover()
	var (
		oc                 = exutil.NewCLI("local-quota", exutil.KubeConfigPath())
		emptyDirPodFixture = exutil.FixturePath("..", "..", "examples", "hello-openshift", "hello-pod.json")
	)

	g.Describe("FSGroup local storage quota", func() {
		g.It("should be applied to XFS filesystem when a pod is created", func() {
			oc.SetOutputDir(exutil.TestContext.OutputDir)
			project := oc.Namespace()

			// TODO: Can be removed when PR linked above merges.
			g.By("updated restricted SCC for fsGroup MustRunAs")
			sccEditErr := setFSGroupMustRunAs(oc)
			o.Expect(sccEditErr).NotTo(o.HaveOccurred())

			// Verify volDir is on XFS, if not this test can't pass:
			g.By("make sure volume directory is on an XFS filesystem")
			volDir := os.Getenv(volDirEnvVar)
			o.Expect(volDir).NotTo(o.Equal(""))
			args := []string{"-f", "-c", "'%T'", volDir}
			outBytes, _ := exec.Command("stat", args...).Output()
			o.Expect(strings.Contains(string(outBytes), "xfs")).To(o.BeTrue())

			g.By("lookup test projects fsGroup ID")
			fsGroup, err := lookupFSGroup(oc, project)
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By("create hello-openshift pod with emptyDir volume")
			_, createPodErr := oc.Run("create").Args("-f", emptyDirPodFixture).Output()
			o.Expect(createPodErr).NotTo(o.HaveOccurred())

			g.By("wait for XFS quota to be applied and verify")
			lookupQuotaErr := waitForQuotaToBeApplied(oc, fsGroup, volDir)
			o.Expect(lookupQuotaErr).NotTo(o.HaveOccurred())
		})
	})
})
