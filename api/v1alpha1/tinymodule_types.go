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

// TinyModuleSpec defines the desired state of TinyModule
type TinyModuleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of TinyModule. Edit tinymodule_types.go to remove/update
	Image string `json:"image,omitempty"`
}

// TinyModuleStatus defines the observed state of TinyModule
type TinyModuleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Addr       string                      `json:"addr"`
	Name       string                      `json:"name"`
	Version    string                      `json:"version"`
	SDKVersion string                      `json:"sdkVersion,omitempty"`
	Components []TinyModuleComponentStatus `json:"components"`
}

type TinyModuleComponentStatus struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Info        string   `json:"info"`
	Tags        []string `json:"tags,omitempty"`

	// Ports carries component-level port metadata (name, direction,
	// JSON schema) so tooling can discover what a component looks like
	// without placing a TinyNode first.
	// +kubebuilder:validation:Optional
	Ports []TinyModuleComponentPort `json:"ports,omitempty"`

	// Examples are short usage snippets (free-form description + code
	// block) that show how to wire this component in common flows.
	// +kubebuilder:validation:Optional
	Examples []string `json:"examples,omitempty"`
}

// TinyModuleComponentPort describes a single port on a component as
// published by the module operator in TinyModule status. This is the
// static, component-level view (independent of any placed TinyNode)
// that lets MCP/LLM tooling inspect port schemas before building flows.
type TinyModuleComponentPort struct {
	// Name is the port identifier (e.g. "request", "response", "out").
	Name string `json:"name"`
	// Label is the human-readable port name.
	// +kubebuilder:validation:Optional
	Label string `json:"label,omitempty"`
	// Source is true for output ports, false for input ports.
	Source bool `json:"source"`
	// Position is the visual port placement hint (Top/Right/Bottom/Left).
	// +kubebuilder:validation:Optional
	Position Position `json:"position,omitempty"`
	// Schema is the JSON schema describing the port's data structure.
	// +kubebuilder:validation:Optional
	Schema []byte `json:"schema,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TinyModule is the Schema for the tinymodules API
type TinyModule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinyModuleSpec   `json:"spec,omitempty"`
	Status TinyModuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TinyModuleList contains a list of TinyModule
type TinyModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinyModule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TinyModule{}, &TinyModuleList{})
}
