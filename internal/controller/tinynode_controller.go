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
	"reflect"
	"sync/atomic"
	"time"

	errors2 "github.com/pkg/errors"
	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TinyNodeReconciler reconciles a TinyNode object
type TinyNodeReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler scheduler.Scheduler
	Module    module.Info
	IsLeader  *atomic.Bool
}

const nodeFinalizer = "io.tinysystems/node-finalizer"

//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinynodes,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinynodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinynodes/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TinyNode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *TinyNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	m, _, err := module.ParseFullName(req.Name)
	if err != nil {
		l.Error(err, "node has invalid name", "name", req.Name)
		return reconcile.Result{}, err
	}

	if m != r.Module.GetNameSanitised() {
		// not us
		return reconcile.Result{}, nil
	}

	l.Info("reconcile", "namespace", req.Namespace, "name", req.Name)

	node := &operatorv1alpha1.TinyNode{}

	if err = r.Get(ctx, req.NamespacedName, node); err != nil {
		l.Error(err, "get tinynode error")
		if errors.IsNotFound(err) {
			// look finalizer logic
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, errors2.Wrap(err, "get tinynode error")
	}

	if !node.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(node, nodeFinalizer) {

			if err = r.Scheduler.Destroy(req.Name); err != nil {
				return reconcile.Result{}, errors2.Wrap(err, "unable to destroy scheduler")
			}

			controllerutil.RemoveFinalizer(node, nodeFinalizer)
			if err := r.Update(ctx, node); err != nil {
				return ctrl.Result{}, errors2.Wrap(err, "failed to remove finalizer")
			}
		}

		// Stop reconciliation as the item is being deleted.
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(node, nodeFinalizer) {
		controllerutil.AddFinalizer(node, nodeFinalizer)
		if err := r.Update(ctx, node); err != nil {
			return ctrl.Result{}, errors2.Wrap(err, "failed to add finalizer")
		}
	}

	originNode := node.DeepCopy()

	// update status with current module info
	node.Status.Module = operatorv1alpha1.TinyNodeModuleStatus{
		Version: r.Module.Version,
		Name:    r.Module.Name,
	}

	node.Status.Status = "OK"
	node.Status.Error = false

	ctx = utils.WithLeader(ctx, r.IsLeader.Load())

	// upsert in scheduler
	err = r.Scheduler.Update(ctx, node)
	if err != nil {
		return reconcile.Result{}, errors2.Wrap(err, "failed to update scheduler")
	}

	// Mark that we've processed this generation - clients can use this to know
	// when the controller has processed a specific spec version (including settings)
	node.Status.ObservedGeneration = node.ObjectMeta.Generation

	// Only leader updates status to avoid conflicts
	if !r.IsLeader.Load() {
		return ctrl.Result{}, nil
	}

	// Only update status if it changed
	if reflect.DeepEqual(originNode.Status, node.Status) {
		return ctrl.Result{
			RequeueAfter: time.Minute * 5,
		}, nil
	}

	t := v1.NewTime(time.Now())
	node.Status.LastUpdateTime = &t

	// update status
	err = r.Status().Patch(ctx, node, client.MergeFrom(originNode))
	if err != nil {
		l.Error(err, "status update error")
		return reconcile.Result{}, errors2.Wrap(err, "status update error")
	}

	return ctrl.Result{
		RequeueAfter: time.Minute * 5,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinyNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinyNode{}).
		// Reconcile on spec changes (generation) OR status.metadata changes
		// This allows non-leaders to pick up metadata updates from the leader
		WithEventFilter(GenerationOrMetadataChangedPredicate{}).
		Complete(r)
}
