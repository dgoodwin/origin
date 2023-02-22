package allowedalerts

import (
	"reflect"

	"github.com/openshift/origin/pkg/synthetictests/platformidentification"
	"github.com/sirupsen/logrus"
)

// AllAlertTests returns the list of AlertTests with independent tests instead of relying on a backstop test.
// etcdAllowance can be the DefaultAllowances, but the quality of testing will be better if it is set.
// Some callers do not intend to run these tests (rather only to list alerts which have a test),
// in which case JobType can be an empty struct.
func AllAlertTests(jobType *platformidentification.JobType, etcdAllowance AlertTestAllowanceCalculator) []AlertTest {

	ret := []AlertTest{}
	ret = append(ret, newWatchdogAlert(jobType))

	// Generate an individual test for every alert we have that matches this job, and has at least 100 runs.
	// Possible future improvements:
	//   - send the number of times the alert has actually fired in the PRs from ci-tools repo, not just the number of runs since we know it existed
	//   - only add an individual test if it's fired more than X times
	//   - perhaps add a test if it has more than 50 runs, but flake if it fails its threshold and is under 100 runs
	//
	// When a new alert appears we haven't seen before in the release, it will not get it's own test until we've accumulated 100 runs since, and
	// transferred this data to origin's data file. Until then it is caught by the backstop test.

	for k, v := range getCurrentResults().HistoricalData {
		if reflect.DeepEqual(k.JobType, *jobType) && v.JobRuns >= 100 {
			ret = append(ret, newAlertBuilder("unknown", v.AlertName, v.AlertNamespace, jobType).toTests()...)
		}
	}
	logrus.Infof("created %d individual alert+namespace tests based on those with sufficient historical data for this job", len(ret))

	/*
		ret = append(ret, newNamespacedAlert("KubePodNotReady", jobType).toTests()...)

		ret = append(ret, newAlertBuilder("etcd", "etcdMembersDown", jobType).toTests()...)
		ret = append(ret, newAlertBuilder("etcd", "etcdGRPCRequestsSlow", jobType).toTests()...)
		ret = append(ret, newAlertBuilder("etcd", "etcdHighNumberOfFailedGRPCRequests", jobType).toTests()...)
		ret = append(ret, newAlertBuilder("etcd", "etcdMemberCommunicationSlow", jobType).toTests()...)
		ret = append(ret, newAlertBuilder("etcd", "etcdNoLeader", jobType).toTests()...)
		ret = append(ret, newAlertBuilder("etcd", "etcdHighFsyncDurations", jobType).toTests()...)
		ret = append(ret, newAlertBuilder("etcd", "etcdHighCommitDurations", jobType).toTests()...)
		ret = append(ret, newAlertBuilder("etcd", "etcdInsufficientMembers", jobType).toTests()...)

		// This test gets a little special treatment, if we're moving through etcd updates, we expect leader changes, so if this scenario is detected
		// this test is given fixed leeway for the alert to fire, otherwise it too falls back to historical data.
		ret = append(ret, newAlertBuilder("etcd", "etcdHighNumberOfLeaderChanges", jobType).withAllowance(etcdAllowance).toTests()...)

		ret = append(ret, newAlertBuilder("kube-apiserver", "KubeAPIErrorBudgetBurn", jobType).toTests()...)
		ret = append(ret, newAlertBuilder("kube-apiserver", "KubeClientErrors", jobType).toTests()...)

		ret = append(ret, newAlertBuilder("storage", "KubePersistentVolumeErrors", jobType).toTests()...)

		ret = append(ret, newAlertBuilder("machine config operator", "MCDDrainError", jobType).toTests()...)

		ret = append(ret, newAlertBuilder("machine config operator", "MCDPivotError", jobType).toTests()...)

		ret = append(ret, newAlertBuilder("monitoring", "PrometheusOperatorWatchErrors", jobType).toTests()...)

		ret = append(ret, newAlertBuilder("OLM", "RedhatOperatorsCatalogError", jobType).toTests()...)

		ret = append(ret, newAlertBuilder("storage", "VSphereOpenshiftNodeHealthFail", jobType).neverFail().toTests()...) // https://bugzilla.redhat.com/show_bug.cgi?id=2055729

		ret = append(ret, newAlertBuilder("samples", "SamplesImagestreamImportFailing", jobType).toTests()...)

	*/

	return ret
}
