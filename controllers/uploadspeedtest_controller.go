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
	"github.com/openshift/oadp-operator/pkg/credentials"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"regexp"
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

	// fetch the credentials from the secret
	secret, err := r.getProviderSecret(ust.Spec.CloudProviderSecretRef.Name)
	if err != nil {
		r.Log.Error(err, "failed to get provider secret")
		return nil, err
	}

	switch providerName {
	case AWSProvider:
		_, secretKey, _ := r.getSecretNameAndKey(ust.Spec.BackupLocation.Velero.Config, ust.Spec.BackupLocation.Velero.Credential, oadpv1alpha1.DefaultPluginAWS)
		awsProfile := "default"
		if value, exists := ust.Spec.BackupLocation.Velero.Config[Profile]; exists {
			awsProfile = value
		}
		accessKeyID, secretAccessKey, err := r.parseAWSSecret(secret, secretKey, awsProfile)
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

func (r *UploadSpeedTestReconciler) getProviderSecret(secretName string) (corev1.Secret, error) {

	secret := corev1.Secret{}
	key := types.NamespacedName{
		Name:      secretName,
		Namespace: r.NamespacedName.Namespace,
	}
	err := r.Get(r.Context, key, &secret)

	if err != nil {
		return secret, err
	}
	originalSecret := secret.DeepCopy()
	// replace carriage return with new line
	secret.Data = replaceCarriageReturn(secret.Data, r.Log)
	r.Client.Patch(r.Context, &secret, client.MergeFrom(originalSecret))
	return secret, nil
}

func (r *UploadSpeedTestReconciler) parseAWSSecret(secret corev1.Secret, secretKey string, matchProfile string) (string, string, error) {

	AWSAccessKey, AWSSecretKey, profile := "", "", ""
	splitString := strings.Split(string(secret.Data[secretKey]), "\n")
	keyNameRegex, err := regexp.Compile(`\[.*\]`)
	const (
		accessKeyKey = "aws_access_key_id"
		secretKeyKey = "aws_secret_access_key"
	)
	if err != nil {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("parseAWSSecret faulty regex: keyNameRegex")
	}
	awsAccessKeyRegex, err := regexp.Compile(`\b` + accessKeyKey + `\b`)
	if err != nil {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("parseAWSSecret faulty regex: awsAccessKeyRegex")
	}
	awsSecretKeyRegex, err := regexp.Compile(`\b` + secretKeyKey + `\b`)
	if err != nil {
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("parseAWSSecret faulty regex: awsSecretKeyRegex")
	}
	for index, line := range splitString {
		if line == "" {
			continue
		}
		if keyNameRegex.MatchString(line) {
			awsProfileRegex, err := regexp.Compile(`\[|\]`)
			if err != nil {
				return AWSAccessKey, AWSSecretKey, fmt.Errorf("parseAWSSecret faulty regex: keyNameRegex")
			}
			cleanedLine := strings.ReplaceAll(line, " ", "")
			parsedProfile := awsProfileRegex.ReplaceAllString(cleanedLine, "")
			if parsedProfile == matchProfile {
				profile = matchProfile
				// check for end of arr
				if index+1 >= len(splitString) {
					break
				}
				for _, profLine := range splitString[index+1:] {
					if profLine == "" {
						continue
					}
					matchedAccessKey := awsAccessKeyRegex.MatchString(profLine)
					matchedSecretKey := awsSecretKeyRegex.MatchString(profLine)

					if err != nil {
						r.Log.Info("Error finding access key id for the supplied AWS credential")
						return AWSAccessKey, AWSSecretKey, err
					}
					if matchedAccessKey { // check for access key
						AWSAccessKey, err = r.getMatchedKeyValue(accessKeyKey, profLine)
						if err != nil {
							r.Log.Info("Error processing access key id for the supplied AWS credential")
							return AWSAccessKey, AWSSecretKey, err
						}
						continue
					} else if matchedSecretKey { // check for secret key
						AWSSecretKey, err = r.getMatchedKeyValue(secretKeyKey, profLine)
						if err != nil {
							r.Log.Info("Error processing secret key id for the supplied AWS credential")
							return AWSAccessKey, AWSSecretKey, err
						}
						continue
					} else {
						break // aws credentials file is only allowed to have profile followed by aws_access_key_id, aws_secret_access_key
					}
				}
			}
		}
	}
	if profile == "" {
		r.Log.Info("Error finding AWS Profile for the supplied AWS credential")
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("error finding AWS Profile for the supplied AWS credential")
	}
	if AWSAccessKey == "" {
		r.Log.Info("Error finding access key id for the supplied AWS credential")
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("error finding access key id for the supplied AWS credential")
	}
	if AWSSecretKey == "" {
		r.Log.Info("Error finding secret access key for the supplied AWS credential")
		return AWSAccessKey, AWSSecretKey, fmt.Errorf("error finding secret access key for the supplied AWS credential")
	}

	return AWSAccessKey, AWSSecretKey, nil
}

// Return value to the right of = sign with quotations and spaces removed.
func (r *UploadSpeedTestReconciler) getMatchedKeyValue(key string, s string) (string, error) {
	for _, removeChar := range []string{"\"", "'", " "} {
		s = strings.ReplaceAll(s, removeChar, "")
	}
	for _, prefix := range []string{key, "="} {
		s = strings.TrimPrefix(s, prefix)
	}
	if len(s) == 0 {
		r.Log.Info(fmt.Sprintf("Could not parse secret for %s", key))
		return s, fmt.Errorf("secret parsing error in key " + key)
	}
	return s, nil
}

func (r *UploadSpeedTestReconciler) getSecretNameAndKey(config map[string]string, credential *corev1.SecretKeySelector, plugin oadpv1alpha1.DefaultPlugin) (string, string, error) {
	// Assume default values unless user has overriden them
	secretName := credentials.PluginSpecificFields[plugin].SecretName
	secretKey := credentials.PluginSpecificFields[plugin].PluginSecretKey
	if _, ok := config["credentialsFile"]; ok {
		if secretName, secretKey, err :=
			credentials.GetSecretNameKeyFromCredentialsFileConfigString(config["credentialsFile"]); err == nil {
			r.Log.Info(fmt.Sprintf("credentialsFile secret: %s, key: %s", secretName, secretKey))
			return secretName, secretKey, nil
		}
	}
	// check if user specified the Credential Name and Key
	if credential != nil {
		if len(credential.Name) > 0 {
			secretName = credential.Name
		}
		if len(credential.Key) > 0 {
			secretKey = credential.Key
		}
	}

	err := r.verifySecretContent(secretName, secretKey)
	if err != nil {
		return secretName, secretKey, err
	}

	return secretName, secretKey, nil
}

func (r *UploadSpeedTestReconciler) verifySecretContent(secretName string, secretKey string) error {
	secret, err := r.getProviderSecret(secretName)
	if err != nil {
		return err
	}
	data, foundKey := secret.Data[secretKey]
	if !foundKey || len(data) == 0 {
		return fmt.Errorf("Secret name %s is missing data for key %s", secretName, secretKey)
	}
	return nil
}
