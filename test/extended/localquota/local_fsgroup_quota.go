package localquota

import (
	"bytes"
	"errors"
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

func getEnvVar(key string) string {
	for _, e := range os.Environ() {
		pair := strings.Split(e, "=")
		if pair[0] == key {
			return pair[1]
		}
	}
	return ""
}

// setFSGroupMustRunAs flips the restricted SCC's FSGroup setting
// to MustRunAs. May not be required for long as this will soon be
// the default.
func setFSGroupMustRunAs(oc *exutil.CLI) error {
	// Write out to temp file so we can edit the SCC and update it:
	tempFile, err := ioutil.TempFile("", "fsgroup-scc-edit")
	defer os.Remove(tempFile.Name())
	fmt.Printf("Created temp file: %s\n", tempFile.Name())

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
	fmt.Printf("Replace output: %s\n", string(outBytes))
	return nil
}

func lookupFSGroup(oc *exutil.CLI, project string) (int, error) {
	//oc get project/default --template='{{ index .metadata.annotations "openshift.io/sa.scc.supplemental-groups" }}'
	gidRange, err := oc.Run("get").Args("project", project, "--template='{{ index .metadata.annotations \"openshift.io/sa.scc.supplemental-groups\" }}'").Output()
	fmt.Println(err)
	if err != nil {
		return 0, err
	}
	fmt.Println(gidRange)

	// gidRange will be something like: 1000030000/10000
	fsGroupStr := strings.Split(gidRange, "/")[0]
	fsGroupStr = strings.Replace(fsGroupStr, "'", "", -1)

	fsGroup, err := strconv.Atoi(fsGroupStr)
	if err != nil {
		return 0, err
	}

	return fsGroup, nil
}

// Sample XFS quota report output:
// $ xfs_quota -x -c 'report -n' /tmp/openshift
// Group quota on /tmp/openshift (/dev/sdb2)
//                               Blocks
// Group ID         Used       Soft       Hard    Warn/Grace
// ---------- --------------------------------------------------
// #0              99004          0          0     00 [--------]
// #1000          166916  268435456  268435456     00 [--------]

// lookupXFSQuota runs an xfs_quota report and parses the output
// looking for the given fsGroup ID's hard quota.
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
	fmt.Printf("Volume directory is on: %s\n", fsDevice)

	args := []string{"xfs_quota", "-x", "-c",
		fmt.Sprintf("report -n -L %d -U %d", fsGroup, fsGroup),
		fsDevice}
	fmt.Printf("%s\n", args)
	cmd := exec.Command("sudo", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outBytes, err = cmd.Output()
	if err != nil {
		return 0, err
	}
	fmt.Printf("stderr: %s\n", stderr.String())
	quotaReport := string(outBytes)
	fmt.Printf("Got XFS Quota report: \n%s\n", quotaReport)

	// Parse output looking for lines starting with a #, which are the lines with
	// group IDs and their quotas:
	lines := strings.Split(quotaReport, "\n")
	var quotaFound int
	for _, l := range lines {
		fmt.Printf("Got line: %s\n", l)
		if strings.HasPrefix(l, fmt.Sprintf("#%d", fsGroup)) {
			fmt.Println("Found!")
			words := strings.Fields(l)
			fmt.Printf("Words: %s\n", words)
			if len(words) != 6 {
				return 0, fmt.Errorf("expected 6 words in quota line: %s", l)
			}
			quotaFound, err := strconv.Atoi(words[3])
			if err != nil {
				return 0, err
			}
			return quotaFound, nil
		}
	}
	if quotaFound == 0 {
		return 0, errors.New("no quota found in allocated time")
	}

	return quotaFound, nil
}

var _ = g.Describe("[volumes] Test local storage quota", func() {
	defer g.GinkgoRecover()
	const (
		volumeDirVar = "VOLUME_DIR"
		projectName  = "local-quota"
	)
	var (
		oc                 = exutil.NewCLI(projectName, exutil.KubeConfigPath())
		emptyDirPodFixture = exutil.FixturePath("..", "..", "examples", "hello-openshift", "hello-pod.json")
	)

	// TODO: Before we call this test, we need to modify node-config.yaml:
	//
	// volumeConfig:
	//   localQuota:
	//     fsGroup: 256Mi
	//
	// This may imply a new launcher script, there are a couple examples, but none seem to
	// call golang test code...
	g.Describe("FSGroup local storage quota", func() {
		g.It("should be applied to XFS filesystem when a pod is created", func() {
			fmt.Println("\n################ Running local storage quota tests")
			fmt.Println(exutil.TestContext.OutputDir)
			oc.SetOutputDir(exutil.TestContext.OutputDir)
			fmt.Println(oc.Namespace())
			project := oc.Namespace()

			// TODO: Modify appropriate SCC (presumably restricted) to set FSGroup to "MustRunAs"
			// This may not be necessary once this merges: https://github.com/openshift/origin/pull/7334
			g.By("updated restricted SCC for fsGroup MustRunAs")
			sccEditErr := setFSGroupMustRunAs(oc)
			o.Expect(sccEditErr).NotTo(o.HaveOccurred())

			g.By("make sure volume directory is on an XFS filesystem")
			volDir := getEnvVar(volumeDirVar)
			o.Expect(volDir).NotTo(o.Equal(""))
			// Verify volDir is on XFS, if not this test can't pass:
			// Use pre-existing utility in the empty_dir quota.go.
			fmt.Printf("volDir = %s\n", volDir)
			args := []string{"-f", "-c", "'%T'", volDir}
			outBytes, _ := exec.Command("stat", args...).Output()
			// If the volume directory is not on an XFS filesystem, this test cannot pass,
			// so we'll fail it early.
			o.Expect(strings.Contains(string(outBytes), "xfs")).To(o.BeTrue())

			// Lookup the fsgroup for the pod's project. (first group ID in the supplemental range)
			fsGroup, err := lookupFSGroup(oc, project)
			o.Expect(err).NotTo(o.HaveOccurred())
			fmt.Printf("Found fsGroup for project: %d\n", fsGroup)

			// TODO: Create a template that has an emptyDir volume, as simple as possible.
			// Use hello-pod.json from examples?
			g.By("create simple pod with emptyDir volume")
			output, createPodErr := oc.Run("create").Args("-f", emptyDirPodFixture).Output()
			o.Expect(createPodErr).NotTo(o.HaveOccurred())
			fmt.Println(output)

			// We need to wait for the pod to be created:
			g.By("wait for pod to be created")
			time.Sleep(20 * time.Second)

			// TODO: Check the filesystem xfs quota report for our fsgroup ID and appropriate quota set.
			// xfs_quota -x -c 'report -n  -L 1000000000 -U 1000080000' volDir
			g.By("verify XFS quota was applied")
			quota, quotaErr := lookupXFSQuota(oc, fsGroup, volDir)
			o.Expect(quotaErr).NotTo(o.HaveOccurred())
			fmt.Printf("Got quota: %d\n", quota)

			// We applied 256Mi, xfs_quota reports in Kb:
			o.Expect(quota).To(o.Equal(262144))
		})
	})
})
