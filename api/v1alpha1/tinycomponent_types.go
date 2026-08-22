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

// A component whose definition is data rather than a compiled binary.
//
// Every other component is Go source: built into a module image, pushed to a
// registry, entered in a catalogue, installed into a cluster. Changing one
// means running that whole chain. That cost is why the palette grows slowly and
// why, when people need something the palette lacks, they reach for js_eval and
// write the logic inline — which measurement bears out: js_eval is one node in
// five, and twenty-eight of its twenty-nine uses do nothing but reshape data
// between two other components.
//
// An inline script cannot be named, so the same reshape is rewritten node after
// node and a fix reaches only the copy in front of you. This is the same thing
// with a name: ports, behaviour and an error boundary, held as a resource that
// nodes REFERENCE. Editing it changes every instance, and defining one needs no
// build, no image and no release — which also means an agent can create one
// while a flow is being assembled.
//
// It does not replace compiled modules. Those exist to reach the outside world
// — the Kubernetes API, HTTP, gRPC, model providers, databases — and need real
// libraries and credentials. This covers what sits between them.

// TinyComponentSpec defines a component implemented by a script.
type TinyComponentSpec struct {
	// Component is the name flows reference in TinyNode.Spec.Component, e.g.
	// "summarise_pods". Conventionally snake_case, like compiled components,
	// because nothing downstream can tell the two apart — which is the point.
	//
	// It must not collide with a compiled component's name: the module serving
	// it refuses the definition rather than shadowing something that already
	// works.
	Component string `json:"component"`

	// Description is the one-line summary shown in the palette.
	// +kubebuilder:validation:Optional
	Description string `json:"description,omitempty"`

	// Info is the longer explanation an agent reads when deciding whether this
	// is the component it needs.
	// +kubebuilder:validation:Optional
	Info string `json:"info,omitempty"`

	// Runtime names the module that executes this definition. "js" is served by
	// js-module. A definition whose runtime no module claims is simply never
	// installed — it is not an error, because the module may be installed later.
	// +kubebuilder:default=js
	// +kubebuilder:validation:Optional
	Runtime string `json:"runtime,omitempty"`

	// Script is the implementation. For the js runtime this is an ES module
	// with a default export taking the input object and returning the output.
	Script string `json:"script"`

	// Input is the JSON Schema of the request port — the shape upstream edges
	// must produce. Required, because an edge cannot be validated against a
	// shape nobody declared.
	Input []byte `json:"input"`

	// Output is the JSON Schema of the response port — the shape downstream
	// edges may read. Required for the same reason in the other direction:
	// without it every edge leaving this component is unverifiable.
	Output []byte `json:"output"`

	// EnableErrorPort exposes an `error` port so a caller can catch failures
	// instead of letting them abort the run. Same contract as every compiled
	// component's error boundary.
	// +kubebuilder:validation:Optional
	EnableErrorPort bool `json:"enableErrorPort,omitempty"`
}

// TinyComponentStatus reports whether a module picked the definition up.
type TinyComponentStatus struct {
	// Installed is true once a module is serving this component. False with a
	// reason is the useful state: a definition nothing serves looks identical
	// to a working one from the outside, which is how a flow ends up
	// referencing a component that will never run.
	// +kubebuilder:validation:Optional
	Installed bool `json:"installed,omitempty"`

	// Module names what is serving it.
	// +kubebuilder:validation:Optional
	Module string `json:"module,omitempty"`

	// Reason explains a false Installed — an unclaimed runtime, a name that
	// collides with a compiled component, a script that would not parse.
	// +kubebuilder:validation:Optional
	Reason string `json:"reason,omitempty"`

	// ObservedGeneration is the spec generation this status describes, so an
	// edit that has not been picked up yet is distinguishable from one that was
	// rejected.
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Component",type=string,JSONPath=`.spec.component`
//+kubebuilder:printcolumn:name="Runtime",type=string,JSONPath=`.spec.runtime`
//+kubebuilder:printcolumn:name="Installed",type=boolean,JSONPath=`.status.installed`
//+kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.reason`

// TinyComponent is the Schema for the tinycomponents API
type TinyComponent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinyComponentSpec   `json:"spec,omitempty"`
	Status TinyComponentStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TinyComponentList contains a list of TinyComponent
type TinyComponentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinyComponent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TinyComponent{}, &TinyComponentList{})
}
