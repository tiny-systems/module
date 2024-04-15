/*
Copyright 2023.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type TinyTrackerPortDataWebhook struct {
	FlowID string `json:"flowID"`

	//Do not send data bigger than below
	MaxDataSize int `json:"maxDataSize"`
}

// TinyTrackerSpec defines the desired state of Tracker
type TinyTrackerSpec struct {
	// +kubebuilder:validation:Optional
	PortDataWebhook *TinyTrackerPortDataWebhook `json:"portDataWebhook"`
}

// TinyTrackerStatus defines the observed state of TinyTracker
type TinyTrackerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TinyTracker is the Schema for the tinytrackers API
type TinyTracker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinyTrackerSpec   `json:"spec,omitempty"`
	Status TinyTrackerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TinyTrackerList contains a list of TinyTracker
type TinyTrackerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinyTracker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TinyTracker{}, &TinyTrackerList{})
}
