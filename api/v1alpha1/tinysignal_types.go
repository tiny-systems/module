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

// TinySignalSpec defines the desired state of TinySignal
type TinySignalSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Node is the name of the TinyNode to signal
	Node string `json:"node"`
	// Port is the port on the node to send the signal to
	Port string `json:"port"`

	// +kubebuilder:validation:Optional
	// Data is the payload to send with the signal
	Data []byte `json:"data"`
}

// TinySignalStatus defines the observed state of TinySignal
type TinySignalStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TinySignal is the Schema for the tinysignals API
type TinySignal struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinySignalSpec   `json:"spec,omitempty"`
	Status TinySignalStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TinySignalList contains a list of TinySignal
type TinySignalList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinySignal `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TinySignal{}, &TinySignalList{})
}
