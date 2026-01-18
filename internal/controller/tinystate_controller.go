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

	// Handle deletion - send nil to component to signal state was deleted
	if !state.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(state, tinyStateFinalizer) {
			l.Info("tinystate being deleted, notifying component", "node", state.Spec.Node, "blocking", isBlockingState)

			// Only notify if instance exists
			if r.Scheduler.HasInstance(state.Spec.Node) {
				leaderCtx := utils.WithLeader(ctx, r.IsLeader.Load())

				// Determine which port to send deletion notification to
				var targetPort string
				var from string
				if isBlockingState {
					targetPort = utils.GetPortFullName(state.Spec.Node, state.Spec.TargetPort)
					from = operatorv1alpha1.BlockingStateFrom
				} else {
					targetPort = utils.GetPortFullName(state.Spec.Node, operatorv1alpha1.StatePort)
					from = runner.FromState
				}

				// Send nil to signal state deletion
				_, _ = r.Scheduler.Handle(leaderCtx, &runner.Msg{
					From: from,
					To:   targetPort,
					Data: nil,
				})
			}

			// Remove finalizer
			controllerutil.RemoveFinalizer(state, tinyStateFinalizer)
			if err := r.Update(ctx, state); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
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
	ctx = utils.WithLeader(ctx, r.IsLeader.Load())

	// Determine target port and from field
	var targetPort string
	var from string
	if isBlockingState {
		// Blocking state - deliver to specific port
		targetPort = utils.GetPortFullName(state.Spec.Node, state.Spec.TargetPort)
		from = operatorv1alpha1.BlockingStateFrom
		l.Info("delivering blocking state", "targetPort", targetPort, "sourceNode", state.Spec.SourceNode)
	} else {
		// Normal state - deliver to _state port
		targetPort = utils.GetPortFullName(state.Spec.Node, operatorv1alpha1.StatePort)
		from = runner.FromState
	}

	// Send state data to the component
	_, err = r.Scheduler.Handle(ctx, &runner.Msg{
		From: from,
		To:   targetPort,
		Data: state.Spec.Data,
	})
	if err != nil {
		l.Error(err, "failed to send state to component")
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
