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
	"github.com/tiny-systems/module/internal/tracker"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
)

// TinyTrackerReconciler reconciles a TinyTracker object
type TinyTrackerReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Manager tracker.Manager
}

//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinytrackers,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinytrackers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.tinysystems.io,resources=tinytrackers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TinyTracker object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *TinyTrackerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	l := log.FromContext(ctx)
	//l.Info("reconcile", "tinytracker", req.Name)

	tracker := &operatorv1alpha1.TinyTracker{}

	err := r.Get(context.Background(), req.NamespacedName, tracker)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			err = r.Manager.Deregister(req.Name)
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	ago := v1.NewTime(time.Now().Add(-time.Minute * 60))

	if tracker.CreationTimestamp.Before(&ago) {
		err = r.Delete(ctx, tracker)
		return reconcile.Result{Requeue: true}, err
	}

	err = r.Manager.Register(*tracker)
	if err != nil {
		// create event maybe?
		//r.Recorder.Event(instance, "Error", "test", "Configuration")
		l.Error(err, "register tracker error")
		return reconcile.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: time.Minute * 60,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinyTrackerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinyTracker{}).
		Complete(r)
}
