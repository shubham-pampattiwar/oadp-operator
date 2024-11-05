/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/openshift/oadp-operator/pkg/cloudprovider"
	"github.com/openshift/oadp-operator/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oadpv1alpha1 "github.com/openshift/oadp-operator/api/v1alpha1"
)

// UploadSpeedTestReconciler reconciles a UploadSpeedTest object
type UploadSpeedTestReconciler struct {
	client.Client
	NamespacedName types.NamespacedName
	Scheme         *runtime.Scheme
	Log            logr.Logger
	Context        context.Context
	EventRecorder  record.EventRecorder
}

//+kubebuilder:rbac:groups=oadp.openshift.io,resources=uploadspeedtests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=oadp.openshift.io,resources=uploadspeedtests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=oadp.openshift.io,resources=uploadspeedtests/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the UploadSpeedTest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *UploadSpeedTestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// set logger and context
	r.Log = log.FromContext(ctx)
	r.Context = ctx
	r.NamespacedName = req.NamespacedName

	r.Log.Info("Reconciling UploadSpeedTest")

	// fetch UploadSpeedTest(UST) CR
	ust := &oadpv1alpha1.UploadSpeedTest{}
	if err := r.Client.Get(ctx, req.NamespacedName, ust); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "unable to fetch UploadSpeedTest")
		return ctrl.Result{}, nil
	}

	r.Log.Info(fmt.Sprintf("Fetched UploadSpeedTest %v", ust))

	// Initialize the provider instance based on BSL provider field
	provider, err := r.initializeProvider(ust)
	if err != nil {
		r.Log.Error(err, "unable to initialize provider")
		return ctrl.Result{}, err
	}

	r.Log.Info(fmt.Sprintf("Parsing file size and test timeout"))
	fileSize, err := parseFileSize(ust.Spec.UploadSpeedTestConfig.FileSize)
	if err != nil {
		r.Log.Error(err, "failed to parse file size")
		return ctrl.Result{}, err
	}

	testTimeout, err := time.ParseDuration(ust.Spec.UploadSpeedTestConfig.TestTimeout)
	if err != nil {
		r.Log.Error(err, "failed to parse test timeout")
		return ctrl.Result{}, err
	}

	r.Log.Info(fmt.Sprintf("Performing uploadspeed test"))
	// perform the upload speed test
	duration, err := provider.UploadTest(r.Context, ust, fileSize, testTimeout)
	if err != nil {
		r.Log.Error(err, "UploadTest failed")
		err = r.updateStatus(r.Context, ust, "Failed", 0, err.Error())
		if err != nil {
			r.Log.Error(err, "Failed to update UploadSpeedTest")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	// Calculate the upload speed (assuming fileSize is in bytes)
	speedMbps := (fileSize * 8) / (duration * 1000)

	r.Log.Info(fmt.Sprintf("Upload test completed in %d ms with speed %d Mbps", duration, speedMbps))

	err = r.updateStatus(r.Context, ust, "Complete", speedMbps, "")
	if err != nil {
		r.Log.Error(err, "Failed to update UploadSpeedTest")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *UploadSpeedTestReconciler) initializeProvider(ust *oadpv1alpha1.UploadSpeedTest) (cloudprovider.CloudProvider, error) {
	providerName := ust.Spec.BackupLocation.Velero.Provider
	region := ust.Spec.BackupLocation.Velero.Config[Region]

	switch providerName {
	case AWSProvider:
		secretName, secretKey, _ := utils.GetSecretNameAndKey(ust.Spec.BackupLocation.Velero.Config, ust.Spec.BackupLocation.Velero.Credential, ust.Namespace, oadpv1alpha1.DefaultPluginAWS, r.Client, r.Context)
		awsProfile := "default"
		if value, exists := ust.Spec.BackupLocation.Velero.Config[Profile]; exists {
			awsProfile = value
		}
		secret, err := utils.GetProviderSecret(secretName, ust.Namespace, r.Client, r.Context)
		if err != nil {
			r.Log.Error(err, "failed to get provider secret")
			return nil, err
		}
		accessKeyID, secretAccessKey, err := utils.ParseAWSSecret(secret, secretKey, awsProfile)
		if err != nil {
			r.Log.Error(err, "failed to parse AWS secret")
			return nil, err
		}
		return cloudprovider.NewAWSProvider(region, accessKeyID, secretAccessKey)
	default:
		return nil, fmt.Errorf("unsupported cloud provider: %s", providerName)
	}
}

// updateStatus is a helper method to update the status of UploadSpeedTest
func (r *UploadSpeedTestReconciler) updateStatus(ctx context.Context, ust *oadpv1alpha1.UploadSpeedTest, status string, speedMbps int64, errorMessage string) error {
	ust.Status.LastTested = metav1.Now()
	ust.Status.Status = status
	ust.Status.SpeedMbps = speedMbps
	ust.Status.ErrorMessage = errorMessage

	// Update the status subresource
	if err := r.Status().Update(ctx, ust); err != nil {
		r.Log.Error(err, "Failed to update UploadSpeedTest status")
		return err
	}
	return nil
}

// Helper function to parse file size
func parseFileSize(sizeStr string) (int64, error) {
	var size int64
	var unit string
	_, err := fmt.Sscanf(sizeStr, "%d%s", &size, &unit)
	if err != nil {
		return 0, fmt.Errorf("invalid file size format: %s", sizeStr)
	}

	unit = strings.ToUpper(unit)

	switch unit {
	case "B":
		return size, nil
	case "KB":
		return size * 1024, nil
	case "MB":
		return size * 1024 * 1024, nil
	case "GB":
		return size * 1024 * 1024 * 1024, nil
	default:
		return 0, fmt.Errorf("unsupported size unit: %s", unit)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *UploadSpeedTestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oadpv1alpha1.UploadSpeedTest{}).
		WithEventFilter(uploadSpeedTestPredicate()).
		Complete(r)
}
