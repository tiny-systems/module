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

package controller

import (
	"reflect"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// StatusMetadataChangedPredicate triggers reconciliation when status.metadata changes.
// This allows non-leader pods to detect when the leader updates metadata (e.g., port info).
type StatusMetadataChangedPredicate struct {
	predicate.Funcs
}

func (StatusMetadataChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldNode, ok := e.ObjectOld.(*operatorv1alpha1.TinyNode)
	if !ok {
		return false
	}

	newNode, ok := e.ObjectNew.(*operatorv1alpha1.TinyNode)
	if !ok {
		return false
	}

	// Trigger if status.metadata changed
	return !reflect.DeepEqual(oldNode.Status.Metadata, newNode.Status.Metadata)
}

// GenerationOrMetadataChangedPredicate combines generation changes with status.metadata changes.
// This ensures reconciliation on:
// - Spec changes (generation bump) - standard behavior
// - Status.metadata changes - allows non-leaders to pick up leader's metadata updates
type GenerationOrMetadataChangedPredicate struct {
	predicate.Funcs
}

func (GenerationOrMetadataChangedPredicate) Create(e event.CreateEvent) bool {
	return true
}

func (GenerationOrMetadataChangedPredicate) Delete(e event.DeleteEvent) bool {
	return !e.DeleteStateUnknown
}

func (GenerationOrMetadataChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	// Check generation change (spec changed)
	if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
		return true
	}

	// Check status.metadata change
	oldNode, ok := e.ObjectOld.(*operatorv1alpha1.TinyNode)
	if !ok {
		return false
	}

	newNode, ok := e.ObjectNew.(*operatorv1alpha1.TinyNode)
	if !ok {
		return false
	}

	return !reflect.DeepEqual(oldNode.Status.Metadata, newNode.Status.Metadata)
}

func (GenerationOrMetadataChangedPredicate) Generic(e event.GenericEvent) bool {
	return false
}
