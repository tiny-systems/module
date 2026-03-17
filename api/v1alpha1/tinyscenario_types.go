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

// ScenarioPortData stores the sample data for a single port
type ScenarioPortData struct {
	// Port is the full port name (e.g., "flowid.module.component-suffix:portname")
	// +kubebuilder:validation:Required
	Port string `json:"port"`

	// Data is the JSON-encoded sample payload for this port
	// +kubebuilder:validation:Optional
	Data []byte `json:"data,omitempty"`
}

// TinyScenarioSpec defines the desired state of TinyScenario
type TinyScenarioSpec struct {
	// Ports contains per-port sample data entries
	// +kubebuilder:validation:Optional
	Ports []ScenarioPortData `json:"ports,omitempty"`
}

// TinyScenarioStatus defines the observed state of TinyScenario
type TinyScenarioStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TinyScenario is the Schema for the tinyscenarios API
type TinyScenario struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinyScenarioSpec   `json:"spec,omitempty"`
	Status TinyScenarioStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TinyScenarioList contains a list of TinyScenario
type TinyScenarioList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinyScenario `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TinyScenario{}, &TinyScenarioList{})
}
