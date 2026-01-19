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
	"context"
	"fmt"
	"sync/atomic"
	"time"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const tinyStateFinalizer = "tinysystems.io/state-cleanup"

// TinyStateReconciler reconciles a TinyState object
type TinyStateReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler scheduler.Scheduler
	Module    module.Info
	IsLeader  *atomic.Bool
	// leadershipCh receives events when leadership changes to trigger requeue of all states
	leadershipCh chan event.GenericEvent
}

//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinystates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinystates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinystates/finalizers,verbs=update

// Reconcile handles TinyState changes and sends state to components.
// For normal states (no TargetPort): sends to _state port
// For blocking states (TargetPort set): sends to the target port, blocks, then requeues
// On leader change, all states are requeued so the new leader can recreate runtime.
func (r *TinyStateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// For blocking states, parse target node name; for normal states, parse from state name
	// Blocking state names are "blocking-{edgeID}", normal state names are "{nodeName}"
	state := &operatorv1alpha1.TinyState{}
	if err := r.Get(ctx, req.NamespacedName, state); err != nil {
		if errors.IsNotFound(err) {
			l.Info("tinystate not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if this state's target node belongs to our module
	targetNode := state.Spec.Node
	m, _, err := module.ParseFullName(targetNode)
	if err != nil {
		// Try parsing from state name (for normal states)
		m, _, err = module.ParseFullName(req.Name)
		if err != nil {
			l.Error(err, "tinystate has invalid name", "name", req.Name)
			return ctrl.Result{}, nil
		}
	}

	if m != r.Module.GetNameSanitised() {
		return ctrl.Result{}, nil
	}

	isBlockingState := state.Spec.TargetPort != ""
	l.Info("reconcile", "namespace", req.Namespace, "name", req.Name, "blocking", isBlockingState)

	// Handle deletion
	if !state.DeletionTimestamp.IsZero() {
		isLeader := r.IsLeader.Load()

		// All pods send nil to stop their local server instance
		if r.Scheduler.HasInstance(state.Spec.Node) {
			l.Info("tinystate being deleted, stopping local instance", "node", state.Spec.Node, "blocking", isBlockingState, "isLeader", isLeader)

			var targetPort, from string
			if isBlockingState {
				targetPort = utils.GetPortFullName(state.Spec.Node, state.Spec.TargetPort)
				from = operatorv1alpha1.BlockingStateFrom
			} else {
				targetPort = utils.GetPortFullName(state.Spec.Node, operatorv1alpha1.StatePort)
				from = runner.FromState
			}

			_, _ = r.Scheduler.Handle(ctx, &runner.Msg{
				From: from,
				To:   targetPort,
				Data: nil,
			})
		}

		// Only leader handles metadata cleanup and finalizer removal
		if !isLeader {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if !controllerutil.ContainsFinalizer(state, tinyStateFinalizer) {
			return ctrl.Result{}, nil
		}

		// Clear target node's metadata for blocking states
		if isBlockingState {
			if err := r.clearNodeMetadata(ctx, state.Spec.Node, state.Namespace); err != nil {
				l.Error(err, "failed to clear target node metadata")
			}
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(state, tinyStateFinalizer)
		if err := r.Update(ctx, state); err != nil {
			return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
		}

		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(state, tinyStateFinalizer) {
		controllerutil.AddFinalizer(state, tinyStateFinalizer)
		if err := r.Update(ctx, state); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Skip if instance doesn't exist yet
	if !r.Scheduler.HasInstance(state.Spec.Node) {
		l.Info("instance not ready, requeuing", "node", state.Spec.Node)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Add leader context so components know if they're the leader
	isLeader := r.IsLeader.Load()
	ctx = utils.WithLeader(ctx, isLeader)

	// For blocking states, only leader delivers to StartPort
	if isBlockingState && !isLeader {
		l.Info("non-leader skipping blocking state delivery", "node", state.Spec.Node)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Determine target port and from field
	var targetPort string
	var from string
	if isBlockingState {
		// Blocking state - deliver to specific port
		targetPort = utils.GetPortFullName(state.Spec.Node, state.Spec.TargetPort)
		// Use SourcePort for edge config lookup, fall back to BlockingStateFrom for backwards compatibility
		if state.Spec.SourcePort != "" {
			from = state.Spec.SourcePort
		} else {
			from = operatorv1alpha1.BlockingStateFrom
		}
		l.Info("delivering blocking state", "targetPort", targetPort, "from", from, "sourceNode", state.Spec.SourceNode)
	} else {
		// Normal state - deliver to _state port
		targetPort = utils.GetPortFullName(state.Spec.Node, operatorv1alpha1.StatePort)
		from = runner.FromState
	}

	// Send state data to the component
	// For blocking states, run in goroutine so reconcile can return and handle deletion
	if isBlockingState {
		// Create fresh context with leader flag (can't use reconcile ctx - it gets cancelled)
		handlerCtx := utils.WithLeader(context.Background(), isLeader)
		go func() {
			_, err := r.Scheduler.Handle(handlerCtx, &runner.Msg{
				From: from,
				To:   targetPort,
				Data: state.Spec.Data,
			})
			if err != nil {
				l.Error(err, "blocking state handler failed")
			}
		}()
	} else {
		_, err = r.Scheduler.Handle(ctx, &runner.Msg{
			From: from,
			To:   targetPort,
			Data: state.Spec.Data,
		})
	}
	if err != nil {
		l.Error(err, "failed to send state to component")

		// For blocking states, update status with error and delete so Signal unblocks
		if isBlockingState && r.IsLeader.Load() {
			l.Info("blocking state failed, updating status and deleting", "error", err.Error())

			// Update status with error info
			originState := state.DeepCopy()
			state.Status.Result = "error"
			if state.Status.Metadata == nil {
				state.Status.Metadata = make(map[string]string)
			}
			state.Status.Metadata["error"] = err.Error()
			_ = r.Status().Patch(ctx, state, client.MergeFrom(originState))

			// Delete the TinyState so Signal's watch unblocks
			if err := r.Delete(ctx, state); err != nil {
				l.Error(err, "failed to delete blocking state after error")
			}
			return ctrl.Result{}, nil
		}

		return ctrl.Result{RequeueAfter: 5 * time.Second}, fmt.Errorf("send state: %w", err)
	}

	// Update status if leader
	if r.IsLeader.Load() {
		originState := state.DeepCopy()
		state.Status.ObservedGeneration = state.ObjectMeta.Generation
		t := metav1.NewTime(time.Now())
		state.Status.LastUpdateTime = &t

		if err := r.Status().Patch(ctx, state, client.MergeFrom(originState)); err != nil {
			return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
		}
	}

	// For blocking states, requeue with backoff to ensure continuous reconciliation
	// This ensures the server keeps running while the TinyState exists
	if isBlockingState {
		l.Info("blocking state delivered, requeuing for continuous reconciliation")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// RequeueAllOnLeadershipChange triggers requeue of all TinyStates when leadership changes.
// This allows the new leader to recreate runtime based on persisted state.
func (r *TinyStateReconciler) RequeueAllOnLeadershipChange() {
	if r.leadershipCh == nil {
		return
	}
	select {
	case r.leadershipCh <- event.GenericEvent{Object: &operatorv1alpha1.TinyState{}}:
	default:
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinyStateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.leadershipCh = make(chan event.GenericEvent, 1)

	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinyState{}).
		WatchesRawSource(source.Channel(
			r.leadershipCh,
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []ctrl.Request {
				var states operatorv1alpha1.TinyStateList
				if err := r.List(ctx, &states); err != nil {
					return nil
				}

				var requests []ctrl.Request
				for _, state := range states.Items {
					m, _, err := module.ParseFullName(state.Spec.Node)
					if err != nil || m != r.Module.GetNameSanitised() {
						continue
					}
					requests = append(requests, ctrl.Request{
						NamespacedName: types.NamespacedName{
							Name:      state.Name,
							Namespace: state.Namespace,
						},
					})
				}
				return requests
			}),
		)).
		Complete(r)
}

// clearNodeMetadata clears all metadata on a node
func (r *TinyStateReconciler) clearNodeMetadata(ctx context.Context, nodeName, namespace string) error {
	var node operatorv1alpha1.TinyNode
	if err := r.Get(ctx, types.NamespacedName{Name: nodeName, Namespace: namespace}, &node); err != nil {
		return err
	}

	if node.Status.Metadata == nil || len(node.Status.Metadata) == 0 {
		return nil
	}

	l := log.FromContext(ctx)
	l.Info("clearing node metadata", "node", nodeName)

	originNode := node.DeepCopy()
	node.Status.Metadata = nil
	return r.Status().Patch(ctx, &node, client.MergeFrom(originNode))
}
