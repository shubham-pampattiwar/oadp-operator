package controllers

import (
	oadpv1alpha1 "github.com/openshift/oadp-operator/api/v1alpha1"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func uploadSpeedTestPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldCR := e.ObjectOld.(*oadpv1alpha1.UploadSpeedTest)
			newCR := e.ObjectNew.(*oadpv1alpha1.UploadSpeedTest)

			return !reflect.DeepEqual(oldCR.Spec, newCR.Spec)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}
