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
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
)

// TinySignalReconciler reconciles a TinySignal object
type TinySignalReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler scheduler.Scheduler
	Module    module.Info
}

//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinysignals,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinysignals/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinysignals/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TinySignal object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *TinySignalReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("reconcile", "tinysignal", req.Name)
	// tiny signal names after name of node it's signaling to

	// to avoid making many queries to Kubernetes API we check name itself against current module
	m, _, err := module.ParseFullName(req.Name)
	if err != nil {
		l.Error(err, "tinysignal has invalid name", "name", req.Name)
		return reconcile.Result{}, err
	}

	if m != r.Module.GetMajorNameSanitised() {
		// not we are
		l.Info("signal is not for the current module", "module", m, "current", r.Module.GetMajorNameSanitised())
		return reconcile.Result{}, nil
	}

	signal := &operatorv1alpha1.TinySignal{}
	err = r.Get(context.Background(), req.NamespacedName, signal)

	if err != nil {
		if errors.IsNotFound(err) {
			// delete signal if related node not found
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if signal.Spec.Port == module.ReconcilePort {
		// we do not use signals for reconcile directly
		_ = r.Delete(ctx, signal)
		return ctrl.Result{}, nil
	}

	// todo add app level context
	if err = r.Scheduler.HandleInternal(context.Background(), &runner.Msg{
		EdgeID: utils.GetPortFullName(signal.Spec.Node, signal.Spec.Port),
		To:     utils.GetPortFullName(signal.Spec.Node, signal.Spec.Port),
		Data:   signal.Spec.Data,
	}); err != nil {
		l.Error(err, "invoke error")
	}

	_ = r.Delete(ctx, signal)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinySignalReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinySignal{}).
		Complete(r)
}
