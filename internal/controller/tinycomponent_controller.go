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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/registry"
)

// TinyComponentReconciler serves components whose definition is a resource.
//
// A definition names a runtime; this module either implements it or does not.
// If it does, the definition is compiled and installed into the scheduler under
// its component name, and every node referencing that name can run. If it does
// not, nothing happens here — another module may serve it, or none may — and
// the status says so, because a definition nothing serves looks identical to a
// working one from the outside. That is how a flow ends up referencing a
// component that will never run.
type TinyComponentReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler scheduler.Scheduler
	Module    module.Info
	IsLeader  *atomic.Bool
}

const componentFinalizer = "io.tinysystems/component-finalizer"

// definedTag marks a component built from a definition. A runtime factory sets
// it so the controller can tell its own installs from a compiled component
// without importing the runtime package.
const definedTag = "Defined"

//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinycomponents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinycomponents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinycomponents/finalizers,verbs=update

func (r *TinyComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	def := &operatorv1alpha1.TinyComponent{}
	if err := r.Get(ctx, req.NamespacedName, def); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get tinycomponent: %w", err)
	}

	name := def.Spec.Component

	// Deletion: withdraw the component and stop anything still running it.
	// Leaving instances alive would keep nodes serving code that no longer
	// exists, which is indistinguishable from working until someone looks.
	if !def.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(def, componentFinalizer) {
			if r.serves(def) && r.Scheduler.Uninstall(name) {
				l.Info("withdrew script-defined component", "component", name)
			}
			controllerutil.RemoveFinalizer(def, componentFinalizer)
			if err := r.Update(ctx, def); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Only take a finalizer for definitions this module actually serves —
	// otherwise every module in the cluster would block deletion of every
	// definition, including ones for runtimes none of them implement.
	if r.serves(def) && !controllerutil.ContainsFinalizer(def, componentFinalizer) {
		controllerutil.AddFinalizer(def, componentFinalizer)
		if err := r.Update(ctx, def); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	installed, reason := r.install(ctx, def)

	// Only the leader writes status, same as every other controller here.
	if !r.IsLeader.Load() {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	origin := def.DeepCopy()
	def.Status.Installed = installed
	def.Status.Reason = reason
	def.Status.ObservedGeneration = def.ObjectMeta.Generation
	if installed {
		def.Status.Module = r.Module.Name
	} else {
		def.Status.Module = ""
	}
	if err := r.Status().Patch(ctx, def, client.MergeFrom(origin)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// serves reports whether this module implements the definition's runtime.
func (r *TinyComponentReconciler) serves(def *operatorv1alpha1.TinyComponent) bool {
	_, ok := registry.GetRuntime(r.runtimeOf(def))
	return ok
}

// runtimeOf defaults an empty runtime to js, matching the CRD's default so a
// definition written without one behaves the same whichever path created it.
func (r *TinyComponentReconciler) runtimeOf(def *operatorv1alpha1.TinyComponent) string {
	if def.Spec.Runtime == "" {
		return "js"
	}
	return def.Spec.Runtime
}

// install compiles and installs the definition, returning whether it is being
// served and why not when it is not.
//
// Every "no" is a sentence rather than a silent skip, because the whole failure
// mode this guards against is a definition that looks fine and never runs.
func (r *TinyComponentReconciler) install(ctx context.Context, def *operatorv1alpha1.TinyComponent) (bool, string) {
	l := log.FromContext(ctx)
	name := def.Spec.Component

	if name == "" {
		return false, "spec.component is empty"
	}

	rt := r.runtimeOf(def)
	factory, ok := registry.GetRuntime(rt)
	if !ok {
		// Not this module's job. Another may serve it; if none does, the flow
		// referencing it will fail visibly at its node rather than here.
		return false, fmt.Sprintf("no module here implements runtime %q", rt)
	}

	// Never shadow a compiled component. Replacing something that already works
	// with something a user typed is the one outcome worth refusing outright,
	// and the name is easy to pick by accident.
	// Reinstalling our own definition is how an edit takes effect, so only a
	// component this module COMPILED blocks the name.
	if existing, taken := r.Scheduler.InstalledComponent(name); taken && !hasTag(existing, definedTag) {
		return false, fmt.Sprintf("%q is already a compiled component in this module", name)
	}

	cmp, err := factory(module.ComponentDefinition{
		Name:            name,
		Description:     def.Spec.Description,
		Info:            def.Spec.Info,
		Script:          def.Spec.Script,
		InputSchema:     []byte(def.Spec.Input),
		OutputSchema:    []byte(def.Spec.Output),
		EnableErrorPort: def.Spec.EnableErrorPort,
	})
	if err != nil {
		// Compilation failed. Reported here, while somebody is looking at the
		// resource they just applied, instead of on the first message.
		return false, err.Error()
	}

	if err := r.Scheduler.Install(cmp); err != nil {
		return false, fmt.Sprintf("install: %v", err)
	}
	l.Info("serving script-defined component", "component", name, "runtime", rt)
	return true, ""
}

// hasTag reports whether a component carries a tag, used to tell a
// script-defined component from a compiled one without importing the runtime.
func hasTag(c module.Component, tag string) bool {
	for _, t := range c.GetInfo().Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func (r *TinyComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinyComponent{}).
		Complete(r)
}
