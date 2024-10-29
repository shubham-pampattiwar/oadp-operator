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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UploadSpeedTestSpec defines the desired state of UploadSpeedTest
type UploadSpeedTestSpec struct {
	// BackupLocation defines relevant configuration of the object storage/backup storage location to be tested
	BackupLocation BackupLocation `json:"backupLocation"`

	// UploadSpeedTestConfig defines the parameters for testing upload speed
	UploadSpeedTestConfig UploadSpeedTestConfig `json:"uploadSpeedTestConfig"`

	// CloudProviderSecretRef is the reference to the secret to be used for authentication with object storage
	CloudProviderSecretRef CloudProviderSecretRef `json:"cloudProviderSecretRef"`
}

type UploadSpeedTestConfig struct {
	// size of the file to be used for test
	FileSize string `json:"fileSize"`

	// timeout value for the upload test operation
	TestTimeout string `json:"testTimeout"`
}

type CloudProviderSecretRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// UploadSpeedTestStatus defines the observed state of UploadSpeedTest
type UploadSpeedTestStatus struct {
	// Timestamp of the last upload speed test
	LastTested metav1.Time `json:"lastTested"`

	// Status of the test operation - Complete, Failed
	Status string `json:"phase"`

	// Upload Speed of the operation
	SpeedMbps int64 `json:"speed"`

	// Details of any error encountered
	ErrorMessage string `json:"errorMessage"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// UploadSpeedTest is the Schema for the uploadspeedtests API
type UploadSpeedTest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UploadSpeedTestSpec   `json:"spec,omitempty"`
	Status UploadSpeedTestStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// UploadSpeedTestList contains a list of UploadSpeedTest
type UploadSpeedTestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UploadSpeedTest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UploadSpeedTest{}, &UploadSpeedTestList{})
}
