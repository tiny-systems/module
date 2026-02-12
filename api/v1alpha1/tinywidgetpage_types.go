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

// TinyWidgetPageSpec defines the desired state of TinyWidgetPage
type TinyWidgetPageSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of TinyWidgetPage. Edit tinywidgetpage_types.go to remove/update
	Widgets []TinyWidget `json:"widgets,omitempty"`
}

// TinyWidgetPageStatus defines the observed state of TinyWidgetPage
type TinyWidgetPageStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TinyWidgetPage is the Schema for the tinywidgetpages API
type TinyWidgetPage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinyWidgetPageSpec   `json:"spec,omitempty"`
	Status TinyWidgetPageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TinyWidgetPageList contains a list of TinyWidgetPage
type TinyWidgetPageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinyWidgetPage `json:"items"`
}

type TinyWidget struct {
	Port        string `json:"port,omitempty"`
	Name        string `json:"name,omitempty"`
	SchemaPatch []byte `json:"schemaPatch,omitempty"`
	GridX       int    `json:"gridX,omitempty"`
	GridY       int    `json:"gridY,omitempty"`
	GridW       int    `json:"gridW,omitempty"`
	GridH       int    `json:"gridH,omitempty"`
	Schema []byte `json:"schema,omitempty"` // Full JSON Schema (content widgets: Port=="")
	Data   []byte `json:"data,omitempty"`   // JSON data (content widgets: Port=="")
}

func init() {
	SchemeBuilder.Register(&TinyWidgetPage{}, &TinyWidgetPageList{})
}
