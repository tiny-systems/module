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

const (
	// flow ID label
	FlowIDLabel = "tinysystems.io/flow-id"
	// module major version label
	ModuleNameMajorLabel = "tinysystems.io/module-version-major"
	// module exact version label
	ModuleVersionLabel = "tinysystems.io/module-version"

	// visual annotations used by platform
	ComponentPosXAnnotation    = "tinysystems.io/component-pos-x"
	ComponentPosYAnnotation    = "tinysystems.io/component-pos-y"
	ComponentPosSpinAnnotation = "tinysystems.io/component-pos-spin"
	CustomLabel                = "tinysystems.io/custom-label"

	SuggestedHttpPortAnnotation = "tinysystems.io/suggested-http-port"

	// ingress annotations
	IngressHostNameSuffixAnnotation      = "tinysystems.io/ingress-hostname-suffix"
	IngressWildcardCertificateAnnotation = "tinysystems.io/ingress-wildcard-certificate"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type (
	Position int
)

// TinyNodeSpec defines the desired state of TinyNode
type TinyNodeSpec struct {

	// Module name - container image repo + tag
	// +kubebuilder:validation:Required
	Module string `json:"module"`

	// Component name within a module
	// +kubebuilder:validation:Required
	Component string `json:"component"`

	// Port configurations
	// +kubebuilder:validation:Optional
	Ports []TinyNodePortConfig `json:"ports"`

	// Edges to send message next
	// +kubebuilder:validation:Optional
	Edges []TinyNodeEdge `json:"edges"`
}

type TinyNodeEdge struct {
	// Edge id
	// +kubebuilder:validation:Required
	ID string `json:"id"`
	// Current node's port name
	// Source port
	// +kubebuilder:validation:Required
	Port string `json:"port"`
	// Other node's full port name
	// +kubebuilder:validation:Required
	To string `json:"to"`
}

type TinyNodePortStatus struct {
	Name          string   `json:"name"`
	Label         string   `json:"label"`
	Position      Position `json:"position"`
	Source        bool     `json:"source"`
	Schema        []byte   `json:"schema"`
	Configuration []byte   `json:"configuration"`
}

type TinyNodePortConfig struct {
	// +kubebuilder:validation:Optional
	// Settings depend on a sender
	From string `json:"from,omitempty"`

	// +kubebuilder:validation:Required
	Port string `json:"port"`

	// +kubebuilder:validation:Optional
	//Schema JSON schema of the port
	Schema []byte `json:"schema"`

	// +kubebuilder:validation:Optional
	//Configuration JSON data of the port's configuration
	Configuration []byte `json:"configuration"`
}

type TinyNodeModuleStatus struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	Version string `json:"version"`
}

type TinyNodeComponentStatus struct {
	// +kubebuilder:validation:Required
	Description string `json:"description"`
	// +kubebuilder:validation:Required
	Info string `json:"info"`
	// +kubebuilder:validation:Optional
	Tags []string `json:"tags"`
}

// TinyNodeStatus defines the observed state of TinyNode
type TinyNodeStatus struct {

	// +kubebuilder:validation:Required
	Module TinyNodeModuleStatus `json:"module"`

	// +kubebuilder:validation:Required
	Component TinyNodeComponentStatus `json:"component"`

	// +kubebuilder:validation:Optional
	Ports []TinyNodePortStatus `json:"ports"`

	// +kubebuilder:validation:Optional
	Status string `json:"status,omitempty"`

	//+kubebuilder:validation:Optional
	Error bool `json:"error,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TinyNode is the Schema for the tinynodes API
type TinyNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinyNodeSpec   `json:"spec,omitempty"`
	Status TinyNodeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TinyNodeList contains a list of TinyNode
type TinyNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinyNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TinyNode{}, &TinyNodeList{})
}
