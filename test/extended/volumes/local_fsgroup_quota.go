package builds

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"

	//"github.com/openshift/origin/pkg/volume/empty_dir"
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

func lookupFSGroup(oc *exutil.CLI, project string) (int, error) {
	out, err := oc.Run("export").Args("project", project).Output()
	fmt.Println(err)
	if err != nil {
		return 0, err
	}
	fmt.Println(out)
	re := regexp.MustCompile("openshift\\.io\\/sa.scc.supplemental-groups: (\\d*)\\/(\\d*)")
	result := re.FindAllStringSubmatch(out, -1)
	if result == nil {
		return 0, fmt.Errorf("unable to parse supplemental-groups from project yaml: %s", out)
	}

	// Should be able to assume there is at least one
	fsGroup, err := strconv.Atoi(result[0][1])
	if err != nil {
		return 0, err
	}

	return fsGroup, nil
}

var _ = g.Describe("[volumes] Test local storage quota", func() {
	defer g.GinkgoRecover()
	const (
		volumeDirVar = "VOLUME_DIR"
		projectName  = "local-quota"
	)
	var (
		oc = exutil.NewCLI(projectName, exutil.KubeConfigPath())
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

			volDir := getEnvVar(volumeDirVar)
			o.Expect(volDir).NotTo(o.Equal(""))
			// TODO: Verify volDir is on XFS? Or just let the test fail.
			// Use pre-existing utility in the empty_dir quota.go.

			// Lookup the fsgroup for the pod's project. (first group ID in the supplemental range)
			fsGroup, err := lookupFSGroup(oc, project)
			o.Expect(err).NotTo(o.HaveOccurred())
			fmt.Printf("Found fsGroup for project: %s\n", fsGroup)

			// TODO: Create a template that has an emptyDir volume, as simple as possible.
			// Use hello-pod.json from examples?

			// TODO: Check the filesystem xfs quota report for our fsgroup ID and appropriate quota set.
			// xfs_quota -x -c 'report -n  -L 1000000000 -U 1000080000' volDir
		})
	})
})
