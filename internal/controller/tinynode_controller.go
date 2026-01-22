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

// TinyNodeReconciler reconciles a TinyNode object
type TinyNodeReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler scheduler.Scheduler
	Module    module.Info
	IsLeader  *atomic.Bool
	// leadershipCh receives events when leadership changes to trigger requeue of all nodes
	leadershipCh chan event.GenericEvent
	// Namespace for resource operations
	Namespace string
}

const nodeFinalizer = "io.tinysystems/node-finalizer"

//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinynodes,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinynodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinynodes/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile moves the current state of the cluster closer to the desired state.
func (r *TinyNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	m, _, err := module.ParseFullName(req.Name)
	if err != nil {
		l.Error(err, "node has invalid name", "name", req.Name)
		return ctrl.Result{}, nil // Don't requeue invalid names
	}

	if m != r.Module.GetNameSanitised() {
		return ctrl.Result{}, nil
	}

	l.Info("reconcile", "namespace", req.Namespace, "name", req.Name)

	node := &operatorv1alpha1.TinyNode{}
	if err := r.Get(ctx, req.NamespacedName, node); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get tinynode: %w", err)
	}

	// Handle deletion
	if !node.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(node, nodeFinalizer) {
			if err := r.Scheduler.Destroy(req.Name); err != nil {
				return ctrl.Result{}, fmt.Errorf("destroy scheduler: %w", err)
			}

			controllerutil.RemoveFinalizer(node, nodeFinalizer)
			if err := r.Update(ctx, node); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if needed
	if !controllerutil.ContainsFinalizer(node, nodeFinalizer) {
		controllerutil.AddFinalizer(node, nodeFinalizer)
		if err := r.Update(ctx, node); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	originNode := node.DeepCopy()

	// Update status with current module info
	node.Status.Module = operatorv1alpha1.TinyNodeModuleStatus{
		Version: r.Module.Version,
		Name:    r.Module.Name,
	}
	node.Status.Status = "OK"
	node.Status.Error = false

	ctx = utils.WithLeader(ctx, r.IsLeader.Load())

	// Update scheduler
	if err := r.Scheduler.Update(ctx, node); err != nil {
		return ctrl.Result{}, fmt.Errorf("update scheduler: %w", err)
	}

	// Mark observed generation
	node.Status.ObservedGeneration = node.ObjectMeta.Generation

	// Only leader updates status
	if !r.IsLeader.Load() {
		return ctrl.Result{}, nil
	}

	// Update timestamp for periodic heartbeat
	t := metav1.NewTime(time.Now())
	node.Status.LastUpdateTime = &t

	if err := r.Status().Patch(ctx, node, client.MergeFrom(originNode)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// RequeueAllOnLeadershipChange triggers requeue of all TinyNodes when this pod becomes leader.
func (r *TinyNodeReconciler) RequeueAllOnLeadershipChange() {
	if r.leadershipCh == nil {
		return
	}
	select {
	case r.leadershipCh <- event.GenericEvent{Object: &operatorv1alpha1.TinyNode{}}:
	default:
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinyNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.leadershipCh = make(chan event.GenericEvent, 1)

	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinyNode{}).
		WithEventFilter(GenerationOrMetadataChangedPredicate{}).
		WatchesRawSource(source.Channel(
			r.leadershipCh,
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []ctrl.Request {
				var nodes operatorv1alpha1.TinyNodeList
				if err := r.List(ctx, &nodes); err != nil {
					return nil
				}

				var requests []ctrl.Request
				for _, node := range nodes.Items {
					m, _, err := module.ParseFullName(node.Name)
					if err != nil || m != r.Module.GetNameSanitised() {
						continue
					}
					requests = append(requests, ctrl.Request{
						NamespacedName: types.NamespacedName{
							Name:      node.Name,
							Namespace: node.Namespace,
						},
					})
				}
				return requests
			}),
		)).
		Complete(r)
}
