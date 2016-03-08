package localquota

import (
	"fmt"
	"testing"

	exutil "github.com/openshift/origin/test/extended/util"
)

// init initialize the extended testing suite.
func init() {
	fmt.Println("############################# A")
	exutil.InitTest()
}

func TestLocalQuota(t *testing.T) {
	fmt.Println("############################# B")
	exutil.ExecuteTest(t, "Local Quota")
}
