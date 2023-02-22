package allowedalerts

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/openshift/origin/pkg/synthetictests/historicaldata"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/origin/pkg/monitor/monitorapi"
	"github.com/openshift/origin/pkg/synthetictests/platformidentification"
	"github.com/openshift/origin/pkg/test/ginkgo/junitapi"
	"k8s.io/kubernetes/test/e2e/framework"
)

type AlertTest interface {
	// InvariantTestName is name for this as an invariant test
	InvariantTestName() string

	// AlertName is the name of the alert
	AlertName() string

	// InvariantCheck performs testing on this alert against the historical data committed to origin repo
	// weekly, with some exceptions for cases we wish to silence.
	InvariantCheck(intervals monitorapi.Intervals, r monitorapi.ResourcesMap) ([]*junitapi.JUnitTestCase, error)
}

type alertBuilder struct {
	bugzillaComponent string
	alertName         string
	alertNamespace    string
	jobType           *platformidentification.JobType

	allowanceCalculator AlertTestAllowanceCalculator
}

type basicAlertTest struct {
	bugzillaComponent string
	alertName         string
	namespace         string
	jobType           *platformidentification.JobType

	allowanceCalculator AlertTestAllowanceCalculator
}

func newAlertBuilder(bugzillaComponent, alertName string, alertNamespace string, jobType *platformidentification.JobType) *alertBuilder {
	return &alertBuilder{
		bugzillaComponent:   bugzillaComponent,
		alertName:           alertName,
		alertNamespace:      alertNamespace,
		allowanceCalculator: DefaultAllowances,
		jobType:             jobType,
	}
}

func (a *alertBuilder) withAllowance(allowanceCalculator AlertTestAllowanceCalculator) *alertBuilder {
	a.allowanceCalculator = allowanceCalculator
	return a
}

func (a *alertBuilder) neverFail() *alertBuilder {
	a.allowanceCalculator = neverFail(a.allowanceCalculator)
	return a
}

func (a *alertBuilder) toTests() []AlertTest {
	return []AlertTest{
		&basicAlertTest{
			bugzillaComponent:   a.bugzillaComponent,
			alertName:           a.alertName,
			namespace:           a.alertNamespace,
			allowanceCalculator: a.allowanceCalculator,
			jobType:             a.jobType,
		},
	}

	/*
		ret := []AlertTest{}
		for namespace, bzComponent := range platformidentification.GetNamespacesToBugzillaComponents() {
			ret = append(ret, &basicAlertTest{
				bugzillaComponent:   bzComponent,
				namespace:           namespace,
				alertName:           a.alertName,
				allowanceCalculator: a.allowanceCalculator,
				jobType:             a.jobType,
			})
		}
		ret = append(ret, &basicAlertTest{
			bugzillaComponent:   "Unknown",
			namespace:           platformidentification.NamespaceOther,
			alertName:           a.alertName,
			allowanceCalculator: a.allowanceCalculator,
			jobType:             a.jobType,
		})

	*/
}

func (a *basicAlertTest) InvariantTestName() string {
	switch {
	case len(a.namespace) == 0:
		return fmt.Sprintf("[bz-%v][invariant] alert/%s should not be firing more than historically", a.bugzillaComponent, a.alertName)
	default:
		return fmt.Sprintf("[bz-%v][invariant] alert/%s should not be firing more than historically in ns/%s", a.bugzillaComponent, a.alertName, a.namespace)
	}
}

func (a *basicAlertTest) AlertName() string {
	return a.alertName
}

type testState int

const (
	pass testState = iota
	flake
	fail
)

func (a *basicAlertTest) failOrFlake(firingIntervals, pendingIntervals monitorapi.Intervals, aLog logrus.FieldLogger) (testState, string) {
	var alertIntervals monitorapi.Intervals

	/*
		switch a.AlertState() {
		case AlertPending:
			alertIntervals = append(alertIntervals, pendingIntervals...)
			fallthrough

		case AlertInfo:
			alertIntervals = append(alertIntervals, firingIntervals.Filter(monitorapi.IsInfoEvent)...)
			fallthrough

		case AlertWarning:
			alertIntervals = append(alertIntervals, firingIntervals.Filter(monitorapi.IsWarningEvent)...)
			fallthrough

		case AlertCritical:
			alertIntervals = append(alertIntervals, firingIntervals.Filter(monitorapi.IsErrorEvent)...)

		default:
			return fail, fmt.Sprintf("unhandled alert state: %v", a.AlertState())
		}

	*/

	describe := alertIntervals.Strings()
	durationAtOrAboveLevel := alertIntervals.Duration(1 * time.Second)
	firingDuration := firingIntervals.Duration(1 * time.Second)
	pendingDuration := pendingIntervals.Duration(1 * time.Second)
	aLog = aLog.WithFields(logrus.Fields{
		"pending": pendingDuration,
		"firing":  firingDuration,
	})

	dataKey := historicaldata.AlertDataKey{
		AlertName:      a.alertName,
		AlertNamespace: a.namespace,
		JobType:        *a.jobType,
	}
	for _, d := range describe {
		aLog.Debugf("alert interval: %s", d)
	}

	// TODO: would be nice to return the level here so we can log it, maybe examine it
	failAfter := a.allowanceCalculator.FailAfter(dataKey)
	if failAfter == nil {
		// TODO: should we skip the test instead rather than give the impression we ran it successfully, with only the logs to indicate otherwise?
		// Or should we run it with whatever data we have but mark it a flake if < 100 runs.
		aLog.Warn("no matching allowance data found with at least 100 job runs, marking test a pass")
		return pass, ""
	}

	aLog = aLog.WithField("failAfter", failAfter)

	if durationAtOrAboveLevel > *failAfter {
		logrus.Warn("test failed")
		return fail, fmt.Sprintf("%s was firing for at least %s on %#v (maxAllowed=%s) which is over the P99 threshold observed for this job over the past several weeks: pending for %s, firing for %s:\n\n%s",
			a.AlertName(), durationAtOrAboveLevel, *a.jobType, failAfter, pendingDuration, firingDuration, strings.Join(describe, "\n"))
	}

	// TODO: should we flake the test if over P95 as well?
	/*
		flakeAfter := a.allowanceCalculator.FlakeAfter(dataKey)
				if durationAtOrAboveLevel > flakeAfter:
					return flake, fmt.Sprintf("%s was at or above %s for at least %s on %#v (maxAllowed=%s): pending for %s, firing for %s:\n\n%s",
						a.AlertName(), a.AlertState(), durationAtOrAboveLevel, *a.jobType, flakeAfter, pendingDuration, firingDuration, strings.Join(describe, "\n"))
				}
	*/

	aLog.Info("test passed")
	return pass, ""
}

var unrecognizedSignatureRegEx = regexp.MustCompile("reason/ErrImagePull UnrecognizedSignatureFormat")

func kubePodNotReadyDueToErrParsingSignature(trackedEventResources monitorapi.InstanceMap, firingIntervals monitorapi.Intervals, aLog logrus.FieldLogger) bool {
	return kubePodNotReadyDueToRegExMatch(trackedEventResources, firingIntervals, unrecognizedSignatureRegEx, aLog)
}

var imagePullBackoffRegEx = regexp.MustCompile("Back-off pulling image .*registry.redhat.io")

// kubePodNotReadyDueToImagePullBackoff returns true if we searched pod events and determined that the
// KubePodNotReady alert for this pod fired due to an imagePullBackoff event on registry.redhat.io.
func kubePodNotReadyDueToImagePullBackoff(trackedEventResources monitorapi.InstanceMap, firingIntervals monitorapi.Intervals, aLog logrus.FieldLogger) bool {
	return kubePodNotReadyDueToRegExMatch(trackedEventResources, firingIntervals, imagePullBackoffRegEx, aLog)
}

func kubePodNotReadyDueToRegExMatch(trackedEventResources monitorapi.InstanceMap, firingIntervals monitorapi.Intervals, regexp *regexp.Regexp, aLog logrus.FieldLogger) bool {
	// Run the check for all firing intervals.
	for _, firingInterval := range firingIntervals {
		relatedPodRef := monitorapi.PodFrom(firingInterval.Locator)

		// Find an event
		foundRegexMatchEvent := false
		var tmpEvent *corev1.Event
		for _, obj := range trackedEventResources {
			tmpEvent = obj.(*corev1.Event)
			if tmpEvent.InvolvedObject.Name == relatedPodRef.Name &&
				tmpEvent.InvolvedObject.Namespace == relatedPodRef.Namespace &&
				regexp.MatchString(tmpEvent.Message) {
				foundRegexMatchEvent = true
				break
			}
		}
		if !foundRegexMatchEvent {
			// No event resources found so we can't do any checking.
			return false
		}
		regexMatchEventTime := tmpEvent.LastTimestamp.Time
		alertTime := firingInterval.From
		if alertTime.After(regexMatchEventTime) && alertTime.Sub(regexMatchEventTime) < time.Minute*10 {
			aLog.Warnf("KubePodNotReady alert failure suppressed due to %s on pod %s/%s", tmpEvent.Message,
				tmpEvent.ObjectMeta.Namespace, tmpEvent.ObjectMeta.Name)
		} else {
			return false
		}
	}
	return true
}

// redhatOperatorPodsNotPending returns true of we determined that there is a redhat-operator
// pod not in Pending state; this implies that the pod is up so we don't need to fail on the
// RedhatOperatorsCatalogError alert.
func redhatOperatorPodsNotPending(trackedPodResources monitorapi.InstanceMap, firingIntervals monitorapi.Intervals) bool {

	// Find the redhat-operators pod in the openshift-marketplace namespace.
	rhPodFound := false
	var rhPod *corev1.Pod
	for _, obj := range trackedPodResources {
		rhPod = obj.(*corev1.Pod)
		if namespace := rhPod.ObjectMeta.Namespace; namespace != "openshift-marketplace" {
			continue
		}
		if podName := rhPod.ObjectMeta.Name; !strings.HasPrefix(podName, "redhat-operators") {
			continue
		}
		rhPodFound = true
	}
	if !rhPodFound {
		// No redhat-operator pod found so we can't do any checking.
		return false
	}

	podStartTime := rhPod.Status.StartTime.Time
	for i := range firingIntervals {
		alertTime := firingIntervals[i].From
		if alertTime.Before(podStartTime) && podStartTime.Sub(alertTime) >= time.Minute*10 && rhPod.Status.Phase != corev1.PodPending {
			framework.Logf("RedhatOperatorsCatalogError alert interval %d failure suppressed since %s is not Pending 10+ minutes later", i, rhPod.ObjectMeta.Name)
		} else {
			return false
		}
	}
	return true
}

func (a *basicAlertTest) InvariantCheck(allEventIntervals monitorapi.Intervals, resourcesMap monitorapi.ResourcesMap) ([]*junitapi.JUnitTestCase, error) {

	aLog := logrus.WithFields(logrus.Fields{
		"alert":     a.alertName,
		"namespace": a.namespace,
	})

	if a.jobType == nil {
		// Hard fail if the higher level job type lookup from the actual cluster failed
		return []*junitapi.JUnitTestCase{
			{
				Name: a.InvariantTestName(),
				FailureOutput: &junitapi.FailureOutput{
					Output: "Unable to determine JobType for alert InvariantCheck",
				},
				SystemOut: "Unable to determine JobType for alert InvariantCheck",
			},
		}, nil
	}
	pendingIntervals := allEventIntervals.Filter(monitorapi.AlertPendingInNamespace(a.alertName, a.namespace))
	firingIntervals := allEventIntervals.Filter(monitorapi.AlertFiringInNamespace(a.alertName, a.namespace))

	testResult, message := a.failOrFlake(firingIntervals, pendingIntervals, aLog)

	switch a.alertName {
	case "KubePodNotReady":
		if testResult == fail && (kubePodNotReadyDueToImagePullBackoff(resourcesMap["events"], firingIntervals, aLog) ||
			kubePodNotReadyDueToErrParsingSignature(resourcesMap["events"], firingIntervals, aLog)) {

			aLog.Warn("flaking test as KubePodNotReady is related to an image pull backoff")
			// Since this is due to imagePullBackoff, change the state to flake instead of fail
			testResult = flake
			break
		}

		// we only care about firing intervals that started before the nodes started updating or ended well after they finished
		nodeUpdates := allEventIntervals.Filter(monitorapi.NodeUpdate)
		if len(nodeUpdates) == 0 {
			break
		}
		earliestUpdateBegan := nodeUpdates[0].From
		lastUpdateFinished := nodeUpdates[len(nodeUpdates)-1].From.Add(15 * time.Minute) /* add grace period to wait for the alert to stop firing */
		firingIntervals = firingIntervals.Filter(
			monitorapi.Or(
				monitorapi.StartedBefore(earliestUpdateBegan),
				monitorapi.EndedAfter(lastUpdateFinished),
			),
		)

		// TODO: would rather we do this filtering before and only check intervals once
		// recheck the state and message.
		testResult, message = a.failOrFlake(firingIntervals, pendingIntervals, aLog)

	case "RedhatOperatorsCatalogError":
		if testResult == fail && redhatOperatorPodsNotPending(resourcesMap["pods"], firingIntervals) {
			testResult = flake
		}
	}

	switch testResult {
	case pass:
		return []*junitapi.JUnitTestCase{
			{
				Name: a.InvariantTestName(),
			},
		}, nil

	case flake:
		return []*junitapi.JUnitTestCase{
			{
				Name: a.InvariantTestName(),
			},
			{
				Name: a.InvariantTestName(),
				FailureOutput: &junitapi.FailureOutput{
					Output: message,
				},
				SystemOut: message,
			},
		}, nil

	case fail:
		return []*junitapi.JUnitTestCase{
			{
				Name: a.InvariantTestName(),
				FailureOutput: &junitapi.FailureOutput{
					Output: message,
				},
				SystemOut: message,
			},
		}, nil

	default:
		return nil, fmt.Errorf("unrecognized state: %v", testResult)
	}
}
