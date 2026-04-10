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
	"time"

	"github.com/go-logr/logr"
	clientpool "github.com/tiny-systems/module/internal/client"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/schema"
	"github.com/tiny-systems/module/registry"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
)

// TinyModuleReconciler reconciles a TinyModule object
type TinyModuleReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Module     module.Info
	ClientPool clientpool.Pool
	IsLeader   *atomic.Bool
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
	l := ctrllog.FromContext(ctx)

	instance := &operatorv1alpha1.TinyModule{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			r.ClientPool.Deregister(req.Name)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Remote module - all pods register to enable sending messages
	if req.Name != r.Module.GetNameSanitised() {
		if instance.Status.Addr != "" {
			r.ClientPool.Register(req.Name, instance.Status.Addr)
		}
		return ctrl.Result{}, nil
	}

	// Own module - only leaders update status
	if !r.IsLeader.Load() {
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}

	instance.Status.Addr = r.Module.Addr
	instance.Status.Version = r.Module.Version
	instance.Status.Name = r.Module.Name
	instance.Status.SDKVersion = r.Module.SDKVersion
	instance.Status.Components = r.buildComponentStatus(l)

	if err := r.Status().Update(ctx, instance); err != nil {
		l.Error(err, "failed to update module status")
		return reconcile.Result{}, err
	}

	return ctrl.Result{}, nil
}

// buildComponentStatus walks the component registry and produces a
// TinyModuleComponentStatus entry per component, including each
// component's ports with their JSON schemas. Publishing port schemas
// at the module level lets tooling (MCP servers, the hosted platform,
// any gRPC client) discover a component's shape without first placing
// a TinyNode.
//
// System ports (_reconcile, _client, _identity) are filtered out —
// they're internal plumbing that external callers don't wire.
func (r *TinyModuleReconciler) buildComponentStatus(l logr.Logger) []operatorv1alpha1.TinyModuleComponentStatus {
	components := registry.Get()
	status := make([]operatorv1alpha1.TinyModuleComponentStatus, len(components))
	for i, cmp := range components {
		info := cmp.GetInfo()
		status[i] = operatorv1alpha1.TinyModuleComponentStatus{
			Name:        info.Name,
			Description: info.Description,
			Info:        info.Info,
			Tags:        info.Tags,
			Ports:       buildComponentPorts(l, cmp, info.Name),
		}
	}
	return status
}

// buildComponentPorts converts a component's Ports() into the
// TinyModuleComponentPort slice published in TinyModule status. Schema
// generation errors are logged and the port is still emitted with an
// empty Schema so tooling can at least know the port exists.
func buildComponentPorts(l logr.Logger, cmp module.Component, componentName string) []operatorv1alpha1.TinyModuleComponentPort {
	ports := cmp.Ports()
	out := make([]operatorv1alpha1.TinyModuleComponentPort, 0, len(ports))
	for _, p := range ports {
		if p.Name == operatorv1alpha1.ReconcilePort ||
			p.Name == operatorv1alpha1.ClientPort ||
			p.Name == operatorv1alpha1.IdentityPort {
			continue
		}

		entry := operatorv1alpha1.TinyModuleComponentPort{
			Name:     p.Name,
			Label:    p.Label,
			Source:   p.Source,
			Position: operatorv1alpha1.Position(p.Position),
		}
		if p.Configuration != nil {
			schemaConf, err := schema.CreateSchema(p.Configuration)
			if err != nil {
				l.Error(err, "buildComponentPorts: schema generation failed",
					"component", componentName, "port", p.Name)
			} else if schemaBytes, mErr := schemaConf.MarshalJSON(); mErr != nil {
				l.Error(mErr, "buildComponentPorts: schema marshal failed",
					"component", componentName, "port", p.Name)
			} else {
				entry.Schema = schemaBytes
			}
		}
		out = append(out, entry)
	}
	return out
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinyModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinyModule{}).
		Complete(r)
}
