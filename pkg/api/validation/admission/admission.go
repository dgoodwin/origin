package admission

import (
	"errors"
	"io"

	"k8s.io/kubernetes/pkg/admission"
	kapi "k8s.io/kubernetes/pkg/api"
	extapi "k8s.io/kubernetes/pkg/apis/extensions"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	runtime "k8s.io/kubernetes/pkg/runtime"

	deployapi "github.com/openshift/origin/pkg/deploy/api"
	sccadmission "github.com/openshift/origin/pkg/security/admission"
	templateapi "github.com/openshift/origin/pkg/template/api"
	"log"
	"reflect"
)

const (
	EmptyDirVolumesDisabledError = "emptyDir volumes disabled in this cluster"
)

func init() {
	admission.RegisterPlugin("BlockDisabledVolumeTypes", func(c client.Interface, config io.Reader) (admission.Interface, error) {
		return NewBlockDisabledVolumeTypes(c), nil
	})
}

type blockVolumeTypes struct {
	*admission.Handler
}

// NewBlockDisabledVolumeTypes returns an admission control to block certain volume types
// as (optionally) defined in master config.
func NewBlockDisabledVolumeTypes(client client.Interface) admission.Interface {
	return sccadmission.NewConstraint(client, true)
	return &blockVolumeTypes{
		Handler: admission.NewHandler(admission.Create, admission.Update),
	}
}

func (b *blockVolumeTypes) Admit(attrs admission.Attributes) error {

	// TODO: currently checks type of every object on every api calls. Filter down on resource?

	//TODO: drop this eventually
	if attrs.GetSubresource() == "status" {
		return nil
	}

	log.Printf("######################### Running admission controller.\n")
	log.Println("  resource = ", attrs.GetResource())
	log.Println("  sub-resource = ", attrs.GetSubresource())
	log.Println("  operation = ", attrs.GetOperation())
	log.Println("  kind = ", attrs.GetKind())
	//log.Println("  object = ", attrs.GetObject())
	log.Println("  object type = ", reflect.TypeOf(attrs.GetObject()).String())
	log.Println("  user name = ", attrs.GetUserInfo().GetName())
	log.Println("  user uid = ", attrs.GetUserInfo().GetUID())
	log.Println("  user groups = ", attrs.GetUserInfo().GetGroups())

	err := checkObject(attrs.GetObject())
	if err != nil {
		return admission.NewForbidden(attrs, err)
	}

	return nil
}

// Checks if the given object is one that we know to potentially contain a
// PodSpec, and if so make sure it doesn't use emptyDir volumes.
func checkObject(obj runtime.Object) error {
	log.Printf("Checking object: type=%s, obj=%s", reflect.TypeOf(obj).String(), obj)
	switch obj.(type) {
	case *templateapi.Template:
		log.Println("YAY we got a template!!!")
		t := obj.(*templateapi.Template)
		log.Printf("%s\n", t)

		// Recurse through the objects in the template:

		// TODO: Need to get from *runtime.Unknown to api objects. Is this the safest way?
		// How bad is the performance hit here? (only called for Template creation, if this
		// plugin is enabled, should be safe)
		runtime.DecodeList(t.Objects, kapi.Scheme)
		for _, item := range t.Objects {
			err := checkObject(item)
			if err != nil {
				return err
			}
		}
	case *deployapi.DeploymentConfig:
		log.Println("Detected create/update of a DeploymentConfig.")
		dc := obj.(*deployapi.DeploymentConfig)
		if &dc.Spec != nil && &dc.Spec.Template != nil && &dc.Spec.Template.Spec != nil {
			err := scanForEmptyDirVolumes(dc.Spec.Template.Spec)
			if err != nil {
				return err
			}
		}
	case *kapi.ReplicationController:
		log.Println("Detected create/update of a ReplicationController.")
		rc := obj.(*kapi.ReplicationController)
		if &rc.Spec != nil && &rc.Spec.Template != nil && &rc.Spec.Template.Spec != nil {
			err := scanForEmptyDirVolumes(rc.Spec.Template.Spec)
			if err != nil {
				return err
			}
		}
	case *kapi.Pod:
		log.Println("Detected create/update of a Pod.")
		p := obj.(*kapi.Pod)
		if &p.Spec != nil {
			err := scanForEmptyDirVolumes(p.Spec)
			if err != nil {
				return err
			}
		}
	case *extapi.Job:
		log.Println("Detected create/update of a Job.")
		j := obj.(*extapi.Job)
		if &j.Spec != nil && &j.Spec.Template != nil && &j.Spec.Template.Spec != nil {
			err := scanForEmptyDirVolumes(j.Spec.Template.Spec)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Checks a PodSpec to ensure there are no emptyDir volumes, if there are we return an API forbidden error.
func scanForEmptyDirVolumes(podSpec kapi.PodSpec) error {
	log.Println("Checking podspec: %s\n", podSpec)
	for _, vol := range podSpec.Volumes {
		log.Printf("Checking a volume: %s\n", &vol.VolumeSource)
		if vol.VolumeSource.EmptyDir != nil {
			return errors.New(EmptyDirVolumesDisabledError)
		}
	}
	return nil
}
