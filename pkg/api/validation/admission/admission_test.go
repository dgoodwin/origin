package admission

import (
	"strings"
	"testing"

	"k8s.io/kubernetes/pkg/admission"
	kapi "k8s.io/kubernetes/pkg/api"
	apierrors "k8s.io/kubernetes/pkg/api/errors"
	extapi "k8s.io/kubernetes/pkg/apis/extensions"
	"k8s.io/kubernetes/pkg/auth/user"
	"k8s.io/kubernetes/pkg/runtime"

	authorizationapi "github.com/openshift/origin/pkg/authorization/api"
	deployapi "github.com/openshift/origin/pkg/deploy/api"
	templateapi "github.com/openshift/origin/pkg/template/api"
)

type testData struct {
	kind          string
	resource      string
	subResource   string
	object        runtime.Object
	response      *authorizationapi.SubjectAccessReviewResponse
	expectedError string // a substring to look for in the error message
}

func TestBlockPod(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:        stubPod(true),
		kind:          "Pod",
		resource:      "pods",
		expectedError: EmptyDirVolumesDisabledError,
	})
}

func TestAllowPodForClusterAdmin(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:        stubPod(true),
		kind:          "Pod",
		resource:      "pods",
		expectedError: EmptyDirVolumesDisabledError,
	})
}

func TestAllowPod(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:   stubPod(false),
		kind:     "Pod",
		resource: "pods",
	})
}

func TestAllowPodNoVolumeSource(t *testing.T) {
	ps := kapi.PodSpec{
		Volumes: []kapi.Volume{
			{
				Name: "no-volume-source",
			},
		},
	}
	pod := &kapi.Pod{
		Spec: ps,
	}
	runAdmissionControllerTest(t, testData{
		object:   pod,
		kind:     "Pod",
		resource: "pods",
	})
}

func TestAllowPodNoVolumes(t *testing.T) {
	ps := kapi.PodSpec{}
	pod := &kapi.Pod{
		Spec: ps,
	}
	runAdmissionControllerTest(t, testData{
		object:   pod,
		kind:     "Pod",
		resource: "pods",
	})
}

func TestBlockDeploymentConfig(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:        stubDeploymentConfig(true),
		kind:          "DeploymentConfig",
		resource:      "deploymentconfigs",
		expectedError: EmptyDirVolumesDisabledError,
	})
}

func TestAllowDeploymentConfig(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:   stubDeploymentConfig(false),
		kind:     "DeploymentConfig",
		resource: "deploymentconfigs",
	})
}

func TestBlockReplicationController(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:        stubReplicationController(true),
		kind:          "ReplicationController",
		resource:      "replicationcontrollers",
		expectedError: EmptyDirVolumesDisabledError,
	})
}

func TestAllowReplicationController(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:   stubReplicationController(false),
		kind:     "ReplicationController",
		resource: "replicationcontrollers",
	})
}

func TestBlockJob(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:        stubJob(true),
		kind:          "Job",
		resource:      "jobs",
		expectedError: EmptyDirVolumesDisabledError,
	})
}

func TestAllowJob(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:   stubJob(false),
		kind:     "Job",
		resource: "jobs",
	})
}

func TestBlockTemplateWithDeploymentConfig(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:        stubTemplate(true),
		kind:          "Template",
		resource:      "templates",
		expectedError: EmptyDirVolumesDisabledError,
	})
}

func TestAllowTemplateWithDeploymentConfig(t *testing.T) {
	runAdmissionControllerTest(t, testData{
		object:   stubTemplate(false),
		kind:     "Template",
		resource: "templates",
	})
}

func runAdmissionControllerTest(t *testing.T, test testData) {
	c := NewBlockDisabledVolumeTypes()
	attrs := admission.NewAttributesRecord(test.object, test.kind, "default", "name", test.resource, test.subResource, admission.Create, stubUser())
	err := c.Admit(attrs)
	if len(test.expectedError) == 0 {
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	} else {
		if err == nil {
			t.Errorf("expected error but got none")
		} else if !apierrors.IsForbidden(err) {
			t.Errorf("expected forbidden error but got %s", err)
		} else if !strings.Contains(err.Error(), test.expectedError) {
			t.Errorf("expected error containing '%s' but got '%s'", test.expectedError, err.Error())
		}
	}
}

func stubUser() user.Info {
	return &user.DefaultInfo{
		Name: "testuser",
	}
}

func stubPodSpec(useEmptyDir bool) kapi.PodSpec {
	ps := kapi.PodSpec{
		Volumes: []kapi.Volume{
			{
				Name:         "empty-dir-volume",
				VolumeSource: kapi.VolumeSource{},
			},
		},
	}
	if useEmptyDir {
		ps.Volumes[0].VolumeSource = kapi.VolumeSource{
			EmptyDir: &kapi.EmptyDirVolumeSource{},
		}
	}

	return ps
}

func stubDeploymentConfig(useEmptyDir bool) *deployapi.DeploymentConfig {
	return &deployapi.DeploymentConfig{
		Spec: deployapi.DeploymentConfigSpec{
			Template: &kapi.PodTemplateSpec{
				Spec: stubPodSpec(useEmptyDir),
			},
		},
	}
}

func stubReplicationController(useEmptyDir bool) *kapi.ReplicationController {
	return &kapi.ReplicationController{
		Spec: kapi.ReplicationControllerSpec{
			Template: &kapi.PodTemplateSpec{
				Spec: stubPodSpec(useEmptyDir),
			},
		},
	}
}

func stubTemplate(useEmptyDir bool) *templateapi.Template {
	return &templateapi.Template{
		Objects: []runtime.Object{
			stubDeploymentConfig(useEmptyDir),
		},
	}
}

func stubJob(useEmptyDir bool) *extapi.Job {
	return &extapi.Job{
		Spec: extapi.JobSpec{
			Template: kapi.PodTemplateSpec{
				Spec: stubPodSpec(useEmptyDir),
			},
		},
	}
}

func stubPod(useEmptyDir bool) *kapi.Pod {
	return &kapi.Pod{
		Spec: stubPodSpec(useEmptyDir),
	}
}
