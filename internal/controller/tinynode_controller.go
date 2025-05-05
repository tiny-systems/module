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
	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/module"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

// TinyNodeReconciler reconciles a TinyNode object
type TinyNodeReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Scheduler scheduler.Scheduler
	Module    module.Info
}

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

	l.Info("reconcile", "tinynode", req.Name)

	node := &operatorv1alpha1.TinyNode{}

	if err = r.Get(context.Background(), req.NamespacedName, node); err != nil {
		l.Error(err, "get tinynode error")
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			// delete signals

			_ = r.DeleteAllOf(context.Background(), &operatorv1alpha1.TinySignal{}, client.InNamespace(req.Namespace), client.MatchingLabels{
				operatorv1alpha1.NodeNameLabel: req.Name,
			})

			if err = r.Scheduler.Destroy(req.Name); err != nil {
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	originNode := node.DeepCopy()

	// select all signals for this node

	signalList := &operatorv1alpha1.TinySignalList{}

	selector, err := v1.LabelSelectorAsSelector(&v1.LabelSelector{
		MatchLabels: map[string]string{
			operatorv1alpha1.NodeNameLabel: node.Name,
		},
	})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("build signal selector error: %s", err)
	}

	if err = r.List(ctx, signalList, client.MatchingLabelsSelector{
		Selector: selector,
	}, client.InNamespace(node.Namespace)); err != nil {
		return reconcile.Result{}, fmt.Errorf("signal list error: %v", err)
	}

	status := &node.Status

	// update status with current module info
	status.Module = operatorv1alpha1.TinyNodeModuleStatus{
		Version: r.Module.Version,
		Name:    r.Module.Name,
	}

	status.Error = false
	status.Status = ""

	t := v1.NewTime(time.Now())

	status.LastUpdateTime = &t
	// upsert in scheduler
	// todo add app level context
	err = r.Scheduler.Update(context.Background(), node, signalList)
	if err != nil {
		l.Error(err, "scheduler upsert error")
		status.Error = true
		status.Status = err.Error()
	}

	node.Status = *status

	err = r.Status().Patch(context.Background(), node, client.MergeFrom(originNode))
	//err = r.Status().Update(context.Background(), node)
	if err != nil {
		l.Error(err, "status update error")
		return reconcile.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: time.Minute * 5,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinyNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinyNode{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&operatorv1alpha1.TinySignal{}, handler.TypedFuncs[client.Object, reconcile.Request]{

			CreateFunc: func(ctx context.Context, e event.TypedCreateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				signal, ok := e.Object.(*operatorv1alpha1.TinySignal)
				if !ok {
					return
				}
				if signal.Spec.Port == module.ReconcilePort {
					// reconcile by signal only applies after deletion of the signal
					return
				}
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      signal.Spec.Node,
						Namespace: signal.Namespace,
					},
				})
			},
			UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				signal, ok := e.ObjectNew.(*operatorv1alpha1.TinySignal)
				if !ok {
					return
				}
				// we do not update reconcile signals, only data ports
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      signal.Spec.Node,
						Namespace: signal.Namespace,
					},
				})
			},
			// when tinySignal deleted signal with reconcile port - reconcile
			DeleteFunc: func(ctx context.Context, e event.TypedDeleteEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				signal, ok := e.Object.(*operatorv1alpha1.TinySignal)
				if !ok {
					return
				}
				// we reconcile if any relates signal deleted
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      signal.Spec.Node,
						Namespace: signal.Namespace,
					},
				})
			}}).
		Complete(r)
}
