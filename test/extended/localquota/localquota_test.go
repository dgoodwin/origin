package localquota

import (
	"testing"

	exutil "github.com/openshift/origin/test/extended/util"
)

func init() {
	exutil.InitTest()
}

func TestLocalQuota(t *testing.T) {
	exutil.ExecuteTest(t, "Local Quota")
}
