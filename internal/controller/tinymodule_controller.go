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
	clientpool "github.com/tiny-systems/module/internal/client"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/registry"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
)

// TinyModuleReconciler reconciles a TinyModule object
type TinyModuleReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Module     module.Info
	ClientPool clientpool.Pool
}

//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinymodules,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinymodules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinymodules/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TinyModule object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *TinyModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	instance := &operatorv1alpha1.TinyModule{}
	err := r.Get(context.Background(), req.NamespacedName, instance)
	if err != nil {
		l.Error(err, "get tinymodule error")
		if errors.IsNotFound(err) {
			r.ClientPool.Deregister(req.Name)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if req.Name != r.Module.GetMajorNameSanitised() {
		// not us, put into table
		r.ClientPool.Register(req.Name, instance.Status.Addr)
	} else {

		instance.Status.Addr = r.Module.Addr
		instance.Status.Version = r.Module.Version
		instance.Status.Name = r.Module.Name

		components := registry.Get()
		statusComponents := make([]operatorv1alpha1.TinyModuleComponentStatus, len(components))
		for i, cmp := range components {
			info := cmp.GetInfo()
			statusComponents[i] = operatorv1alpha1.TinyModuleComponentStatus{
				Name:        info.Name,
				Description: info.Description,
				Info:        info.Info,
				Tags:        info.Tags,
			}
		}
		instance.Status.Components = statusComponents

		err = r.Status().Update(context.Background(), instance)
		if err != nil {
			l.Error(err, "status update error")
			return reconcile.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinyModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinyModule{}).
		Complete(r)
}
