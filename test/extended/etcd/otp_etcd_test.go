package etcd

import (
	"fmt"
	"math/rand"
	"os/exec"
	"regexp"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/origin/test/extended/util"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

// OTP-ported etcd tests from openshift-tests-private (branch: porting-prep).
// Each test is marked [OTP] to indicate its origin.

var _ = g.Describe("[sig-etcd] ETCD [OTP]", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("default-" + otpRandString())

	// port=yes - 99.7% pass rate (584 runs last 60 days)
	g.It("NonHyperShiftHOST [OTP] Add new parameter to avoid Potential etcd inconsistent revision and data occurs", func() {
		g.By("Test for case OCP-52418-Add new parameter to avoid Potential etcd inconsistent revision and data occurs")
		oc.SetupProject()

		e2e.Logf("Discover all the etcd pods")
		etcdPodList := otpGetPodListByLabel(oc, "etcd=true")

		e2e.Logf("get the expected parameter from etcd member pod")
		output, err := oc.AsAdmin().Run("get").Args("-n", "openshift-etcd", "pod", etcdPodList[0], "-o=jsonpath={.spec.containers[*].command[*]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(output, "experimental-initial-corrupt-check=true")).To(o.BeFalse())
	})

	// port=yes - 99.6% pass rate (796 runs last 60 days)
	g.It("NonHyperShiftHOST [OTP] Etcd basic verification", func() {
		g.By("Test for case OCP-24280-Etcd basic verification")
		e2e.Logf("check cluster Etcd operator status")
		otpCheckOperator(oc, "etcd")
		e2e.Logf("verify cluster Etcd operator pod is Running")
		podOprtAllRunning := otpCheckEtcdOperatorPodStatus(oc)
		if podOprtAllRunning != true {
			e2e.Failf("etcd operator pod is not in running state")
		}

		e2e.Logf("retrieve all the master node")
		masterNodeList := otpGetNodeListByLabel(oc, "node-role.kubernetes.io/master=")
		e2e.Logf("Discover all the etcd pods")
		etcdPodList := otpGetPodListByLabel(oc, "etcd=true")
		infraTopologyOutput, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("infrastructures.config.openshift.io", "cluster", "-o=jsonpath={.status.controlPlaneTopology}").Output()
		o.Expect(err).ShouldNot(o.HaveOccurred())
		// Adding the check for TwoNodeArbiter
		if matched, _ := regexp.MatchString("Arbiter", infraTopologyOutput); matched {
			if len(masterNodeList) != len(etcdPodList)-1 {
				e2e.Failf("mismatch in the number of etcd pods and master nodes for Arbiter topology.")
			}
		} else {
			if len(masterNodeList) != len(etcdPodList) {
				e2e.Failf("mismatch in the number of etcd pods and master nodes.")
			}
		}
		e2e.Logf("Ensure all the etcd pods are running")
		podAllRunning := otpCheckEtcdPodStatus(oc)
		if podAllRunning != true {
			e2e.Failf("etcd pods are not in running state")
		}
	})

	// port=yes - 99.5% pass rate (584 runs last 60 days)
	g.It("NonHyperShiftHOST [OTP] New etcd alerts to be added to the monitoring stack in ocp 4.10", func() {
		g.By("Test for case OCP-54129-New etcd alerts to be added to the monitoring stack in ocp 4.10.")
		e2e.Logf("Check new alert msg have been updated")
		output, err := exec.Command("bash", "-c", "oc -n openshift-monitoring get cm prometheus-k8s-rulefiles-0 -oyaml | grep \"alert: etcd\"").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("etcdHighFsyncDurations"))
		o.Expect(output).To(o.ContainSubstring("etcdDatabaseQuotaLowSpace"))
		o.Expect(output).To(o.ContainSubstring("etcdExcessiveDatabaseGrowth"))
	})

	// port=yes - 98.8% pass rate (584 runs last 60 days)
	g.It("NonHyperShiftHOST [OTP] Ensure a safety net for the 3.4 to 3.5 etcd upgrade", func() {
		var (
			err error
			msg string
		)
		g.By("Test for case OCP-43330 Ensure a safety net for the 3.4 to 3.5 etcd upgrade")
		oc.SetupProject()

		e2e.Logf("Discover all the etcd pods")
		etcdPodList := otpGetPodListByLabel(oc, "etcd=true")

		e2e.Logf("verify whether etcd version is 3.5")
		output, err := oc.AsAdmin().WithoutNamespace().Run("rsh").Args("-n", "openshift-etcd", etcdPodList[0], "etcdctl").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		o.Expect(output).To(o.ContainSubstring("3.6"))

		e2e.Logf("get the Kubernetes version")
		version, err := exec.Command("bash", "-c", "oc version | grep Kubernetes |awk '{print $3}'").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		sVersion := string(version)
		kubeVer := strings.Split(sVersion, "+")[0]
		kubeVer = strings.TrimSpace(kubeVer)
		// Sometimes, there will be a version difference between kubeletVersion and k8s version due to RHCOS version.
		// It will be matching once there is a new RHCOS version. Detail see https://issues.redhat.com/browse/OCPBUGS-48612
		pattern := regexp.MustCompile(`\S+?.\d+`)
		validVer := pattern.FindAllString(kubeVer, -1)
		e2e.Logf("Version considered is %v", validVer[0])

		e2e.Logf("retrieve all the master node")
		masterNodeList := otpGetNodeListByLabel(oc, "node-role.kubernetes.io/master=")

		e2e.Logf("verify the kubelet version in node details")
		msg, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("node", masterNodeList[0], "-o", "custom-columns=VERSION:.status.nodeInfo.kubeletVersion").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(msg).To(o.ContainSubstring(validVer[0]))
	})
})

// otpRandString returns an 8-character random lowercase alphanumeric string.
func otpRandString() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	seed := rand.New(rand.NewSource(time.Now().UnixNano()))
	buf := make([]byte, 8)
	for i := range buf {
		buf[i] = chars[seed.Intn(len(chars))]
	}
	return string(buf)
}

func otpGetNodeListByLabel(oc *exutil.CLI, labelKey string) []string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("node", "-l", labelKey, "-o=jsonpath={.items[*].metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return strings.Fields(output)
}

func otpGetPodListByLabel(oc *exutil.CLI, labelKey string) []string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", "openshift-etcd", "-l", labelKey, "-o=jsonpath={.items[*].metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return strings.Fields(output)
}

func otpCheckEtcdPodStatus(oc *exutil.CLI) bool {
	output, err := oc.AsAdmin().Run("get").Args("pods", "-l", "app=etcd", "-n", "openshift-etcd", "-o=jsonpath='{.items[*].status.phase}'").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	for _, podStatus := range strings.Fields(output) {
		if matched, _ := regexp.MatchString("Running", podStatus); !matched {
			e2e.Logf("Find etcd pod is not running")
			return false
		}
	}
	return true
}

func otpCheckEtcdOperatorPodStatus(oc *exutil.CLI) bool {
	output, err := oc.AsAdmin().Run("get").Args("pods", "-n", "openshift-etcd-operator", "-o=jsonpath='{.items[*].status.phase}'").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	for _, podStatus := range strings.Fields(output) {
		if matched, _ := regexp.MatchString("Running", podStatus); !matched {
			e2e.Logf("etcd operator pod is not running")
			return false
		}
	}
	return true
}

func otpCheckOperator(oc *exutil.CLI, operatorName string) {
	err := wait.Poll(60*time.Second, 1500*time.Second, func() (bool, error) {
		output, err := oc.AsAdmin().Run("get").Args("clusteroperator", operatorName).Output()
		if err != nil {
			e2e.Logf("get clusteroperator err, will try next time:\n")
			return false, nil
		}
		if matched, _ := regexp.MatchString("True.*False.*False", output); !matched {
			e2e.Logf("clusteroperator %s is abnormal, will try next time:\n", operatorName)
			return false, nil
		}
		return true, nil
	})
	otpAssertWaitPollNoErr(err, "clusteroperator is abnormal")
}

func otpAssertWaitPollNoErr(e error, msg string) {
	if e == nil {
		return
	}
	var err error
	if strings.Compare(e.Error(), "timed out waiting for the condition") == 0 || strings.Compare(e.Error(), "context deadline exceeded") == 0 {
		err = fmt.Errorf("case: %v\nerror: %s", g.CurrentSpecReport().FullText(), msg)
	} else {
		err = fmt.Errorf("case: %v\nerror: %s", g.CurrentSpecReport().FullText(), e.Error())
	}
	o.Expect(err).NotTo(o.HaveOccurred())
}
