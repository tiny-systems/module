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
	FlowIDLabel                    = "tinysystems.io/flow-id"
	ModuleNameLabel                = "tinysystems.io/module-name"
	ModuleVersionLabel             = "tinysystems.io/module-version"
	ComponentDescriptionAnnotation = "tinysystems.io/component-description"
	ComponentInfoAnnotation        = "tinysystems.io/component-info"
	ComponentTagsAnnotation        = "tinysystems.io/component-tags"

	// visual annotations used by platform
	ComponentPosXAnnotation    = "tinysystems.io/component-pos-x"
	ComponentPosYAnnotation    = "tinysystems.io/component-pos-y"
	ComponentPosSpinAnnotation = "tinysystems.io/component-pos-spin"

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

	//Run if emitter component should run
	// +kubebuilder:validation:Optional
	Run bool `json:"run"`
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
	Settings      bool     `json:"settings"`
	Status        bool     `json:"status"`
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

// TinyNodeStatus defines the observed state of TinyNode
type TinyNodeStatus struct {
	// +kubebuilder:validation:Optional
	Ports []TinyNodePortStatus `json:"ports"`

	// +kubebuilder:validation:Optional
	Error string `json:"error,omitempty"`

	// +kubebuilder:validation:Optional
	Status string `json:"status,omitempty"`

	// +kubebuilder:validation:Optional
	Emitter bool `json:"emitter"`

	// +kubebuilder:validation:Optional
	Emitting bool `json:"emitting"`

	// +kubebuilder:validation:Optional
	Http TinyNodeHttpStatus `json:"http"`
}

type TinyNodeHttpStatus struct {
	// +kubebuilder:validation:Optional
	Available bool `json:"available,omitempty"`

	// +kubebuilder:validation:Optional
	ListenPort int `json:"listenPort"`

	// +kubebuilder:validation:Optional
	PublicURL string `json:"publicURL"`
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
