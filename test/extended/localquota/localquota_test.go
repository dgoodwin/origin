package localquota

import (
	"testing"

	exutil "github.com/openshift/origin/test/extended/util"
)

// init initialize the extended testing suite.
func init() {
	exutil.InitTest()
}

func TestLocalQuota(t *testing.T) {
	exutil.ExecuteTest(t, "Local Quota")
}
