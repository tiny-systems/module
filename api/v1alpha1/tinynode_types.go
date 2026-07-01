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
	//tiny nod elables flow name k8s label
	FlowNameLabel = "tinysystems.io/flow-name"
	//ProjectIDLabel project name k8s label
	ProjectNameLabel = "tinysystems.io/project-name"

	PageTitleAnnotation = "tinysystems.io/page-name"

	PageSortIdxAnnotation = "tinysystems.io/page-sort-idx"
	//
	ProjectNameAnnotation     = "tinysystems.io/project-name"
	FlowDescriptionAnnotation = "tinysystems.io/flow-description"
	//

	// use for tinysignals to attach them to tinynodes
	NodeNameLabel = "tinysystems.io/node-name"

	//ModuleNameMajorLabel module major version label
	ModuleNameMajorLabel = "tinysystems.io/module-version-major"
	DashboardLabel       = "tinysystems.io/dashboard"

	// ExecutionModeLabel opts a node into an execution mode. When set to
	// ExecutionModeDurable, messages arriving at the node's business ports
	// mint (or continue) a durable run: downstream emits are published
	// fire-and-forget to the JetStream work queue with an idempotency key
	// instead of blocking for the subtree's response, so the run survives
	// pod death and migrates across replicas. The platform stamps this on
	// every node of a flow whose execution mode is durable.
	ExecutionModeLabel = "tinysystems.io/execution-mode"
	// ExecutionModeDurable is the ExecutionModeLabel value that enables
	// durable (async, ack-on-emit) execution.
	ExecutionModeDurable = "durable"

	NodeHandlesAnnotation = "tinysystems.io/node-handles"

	SharedWithFlowsAnnotation = "tinysystems.io/shared-with-flows"

	ComponentPosXAnnotation    = "tinysystems.io/component-pos-x"
	ComponentPosYAnnotation    = "tinysystems.io/component-pos-y"
	ComponentPosSpinAnnotation = "tinysystems.io/component-pos-spin"

	NodeLabelAnnotation   = "tinysystems.io/node-label"
	NodeCommentAnnotation = "tinysystems.io/node-comment"

	IngressHostNameSuffixAnnotation = "tinysystems.io/ingress-hostname-suffix"

	ScenarioNameAnnotation = "tinysystems.io/scenario-name"
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

	// +kubebuilder:validation:Required
	FlowID string `json:"flowID"`

	// Retry policy for this edge. Default (MaxAttempts == 0 or 1) =
	// single-shot: the scheduler dispatches once, surface the error.
	// Authors opt into retry per-edge for transient-failure-safe
	// targets (webhooks, idempotent writes). The runtime never silently
	// retries against paid LLM APIs by default — see
	// feedback_no_implicit_retries.md.
	// +kubebuilder:validation:Optional
	RetryPolicy *EdgeRetryPolicy `json:"retryPolicy,omitempty"`
}

// EdgeRetryPolicy controls how the scheduler re-dispatches a single
// edge on handler failure. Matches Temporal-style ActivityOptions in
// spirit, scoped to one edge in a TinyFlow.
//
// On error, the scheduler:
//  1. checks if the error's code is in NonRetryableErrorCodes — if so,
//     surface immediately, no retry.
//  2. otherwise increments the attempt counter and re-dispatches after
//     the policy's backoff, up to MaxAttempts total tries.
type EdgeRetryPolicy struct {
	// Max total dispatch attempts (1 = no retry, the default).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default:=1
	MaxAttempts int `json:"maxAttempts"`

	// Initial backoff between attempts. Default 1s.
	// +kubebuilder:validation:Optional
	InitialDelayMs int `json:"initialDelayMs,omitempty"`

	// Multiplier applied to the delay each attempt. Default 2.0
	// (exponential backoff).
	// +kubebuilder:validation:Optional
	BackoffCoefficient string `json:"backoffCoefficient,omitempty"`

	// Cap on a single attempt's delay. Default 30s.
	// +kubebuilder:validation:Optional
	MaxDelayMs int `json:"maxDelayMs,omitempty"`

	// Error codes that skip retry. Components signal these via
	// module.NonRetryable(code, err) — typically "quota_exceeded",
	// "unauthorized", "content_filter", "validation". The transport
	// stamps `x-error-code` on the reply; the scheduler reads it and
	// short-circuits the retry loop when matched.
	// +kubebuilder:validation:Optional
	NonRetryableErrorCodes []string `json:"nonRetryableErrorCodes,omitempty"`

	// Per-attempt handler timeout in milliseconds. Caps how long a
	// single dispatch attempt can run before the scheduler cancels
	// its context and (if MaxAttempts permits) retries the next one.
	// 0 = use the transport's default (currently 5 minutes for the
	// NATS transports). Bump this on edges that legitimately need
	// long-running handlers — agent planning loops, batch LLM calls,
	// or HTTP probes against slow upstreams.
	// +kubebuilder:validation:Optional
	TimeoutMs int `json:"timeoutMs,omitempty"`
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

	// +kubebuilder:validation:Optional
	FlowID string `json:"flowID,omitempty"`
}

type TinyNodeModuleStatus struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	Version string `json:"version"`
	// +kubebuilder:validation:Optional
	SDKVersion string `json:"sdkVersion,omitempty"`
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
	// ObservedGeneration is the most recent generation observed by the controller.
	// It corresponds to metadata.generation, which is updated on mutation by the API Server.
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +kubebuilder:validation:Required
	Module TinyNodeModuleStatus `json:"module"`

	// +kubebuilder:validation:Required
	Component TinyNodeComponentStatus `json:"component"`

	// +kubebuilder:validation:Optional
	Ports []TinyNodePortStatus `json:"ports"`

	// +kubebuilder:validation:Optional
	Status string `json:"status,omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`

	//+kubebuilder:validation:Optional
	Error bool `json:"error,omitempty"`

	//+kubebuilder:validation:Format:date-time
	//+kubebuilder:validation:Optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
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
