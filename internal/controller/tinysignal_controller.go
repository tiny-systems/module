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
	"sync/atomic"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TinySignalReconciler reconciles a TinySignal object.
// TinySignals are one-off: delivered to the target port, then deleted immediately.
type TinySignalReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler scheduler.Scheduler
	Module    module.Info
	IsLeader  *atomic.Bool
}

//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinysignals,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinysignals/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinysignals/finalizers,verbs=update

// Reconcile handles TinySignal delivery.
// TinySignals are one-off: delivered to the target port, then deleted.
func (r *TinySignalReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Only leader processes signals
	if !r.IsLeader.Load() {
		return ctrl.Result{}, nil
	}

	// Check if this signal belongs to our module
	m, _, err := module.ParseFullName(req.Name)
	if err != nil {
		l.Error(err, "tinysignal has invalid name", "name", req.Name)
		return ctrl.Result{}, nil
	}

	if m != r.Module.GetNameSanitised() {
		return ctrl.Result{}, nil
	}

	var signal operatorv1alpha1.TinySignal
	if err := r.Get(ctx, req.NamespacedName, &signal); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Skip if being deleted
	if !signal.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Deliver the signal
	targetPort := utils.GetPortFullName(signal.Spec.Node, signal.Spec.Port)
	l.Info("delivering signal", "targetPort", targetPort)

	// Wait for target instance to exist (with context timeout)
	if !r.Scheduler.HasInstance(signal.Spec.Node) {
		l.Info("target instance not ready, requeuing", "node", signal.Spec.Node)
		return ctrl.Result{Requeue: true}, nil
	}

	// Deliver - this is fast now since _control ports don't block
	deliveryCtx := utils.WithLeader(ctx, true)
	_, err = r.Scheduler.Handle(deliveryCtx, &runner.Msg{
		From: runner.FromSignal,
		To:   targetPort,
		Data: signal.Spec.Data,
	})

	if err != nil {
		l.Error(err, "delivery failed, will retry")
		return ctrl.Result{Requeue: true}, nil
	}

	l.Info("signal delivered, deleting")

	// Delete the signal (one-off)
	if err := r.Delete(ctx, &signal); err != nil && !errors.IsNotFound(err) {
		l.Error(err, "failed to delete signal after delivery")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// RequeueAllOnLeadershipChange is a no-op for one-off signals.
func (r *TinySignalReconciler) RequeueAllOnLeadershipChange() {
	// No-op: TinySignals are one-off and don't need requeue on leadership change
}

// CancelAllRunningProcesses is a no-op since delivery is now synchronous.
func (r *TinySignalReconciler) CancelAllRunningProcesses() {
	// No-op: delivery is synchronous now
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinySignalReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinySignal{}).
		Complete(r)
}
