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

// TinyStateSpec defines the desired state of TinyState
type TinyStateSpec struct {
	// Node is the name of the TinyNode this state belongs to
	// +kubebuilder:validation:Required
	Node string `json:"node"`

	// Data is the component's runtime state as raw JSON bytes.
	// Components can store any structure they need (e.g., {running: true, context: {...}})
	// +kubebuilder:validation:Optional
	Data []byte `json:"data,omitempty"`

	// TargetPort is the port to deliver to instead of _state (for blocking edges)
	// When set, the controller delivers the data to this specific port
	// +kubebuilder:validation:Optional
	TargetPort string `json:"targetPort,omitempty"`

	// SourceEdgeID is the edge that created this blocking state
	// Used for cleanup when edges are removed
	// +kubebuilder:validation:Optional
	SourceEdgeID string `json:"sourceEdgeID,omitempty"`

	// SourceNode is the owner node that created this blocking state
	// Used for cascade deletion when source node is deleted
	// +kubebuilder:validation:Optional
	SourceNode string `json:"sourceNode,omitempty"`
}

// TinyStateStatus defines the observed state of TinyState
type TinyStateStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastUpdateTime is when the state was last updated
	// +kubebuilder:validation:Format:date-time
	// +kubebuilder:validation:Optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// Result is the completion status for blocking states (completed/cancelled/error)
	// +kubebuilder:validation:Optional
	Result string `json:"result,omitempty"`

	// Metadata contains additional data returned from the target component (e.g., HTTP port)
	// +kubebuilder:validation:Optional
	Metadata map[string]string `json:"metadata,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TinyState is the Schema for the tinystates API.
// It stores runtime state for a TinyNode component, separate from configuration.
// TinyState should be owned by its TinyNode for cascade deletion.
type TinyState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinyStateSpec   `json:"spec,omitempty"`
	Status TinyStateStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TinyStateList contains a list of TinyState
type TinyStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinyState `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TinyState{}, &TinyStateList{})
}
