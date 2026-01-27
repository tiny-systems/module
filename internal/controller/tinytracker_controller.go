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
	"sync/atomic"
	"time"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
)

// TinyTrackerReconciler reconciles a TinyTracker object
type TinyTrackerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Manager  tracker.Manager
	IsLeader *atomic.Bool
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

	// All pods need tracker registration for span data capture
	// Registration is read-only state needed by all replicas to add data to spans

	t := &operatorv1alpha1.TinyTracker{}

	err := r.Get(ctx, req.NamespacedName, t)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found - deregister from local tracker manager
			err = r.Manager.Deregister(req.Name)
		}
		return reconcile.Result{}, err
	}

	// Cleanup old trackers - only leader should delete to avoid multiple API calls
	ago := v1.NewTime(time.Now().Add(-time.Minute * 60))
	if t.CreationTimestamp.Before(&ago) {
		if r.IsLeader.Load() {
			_ = r.Delete(ctx, t)
		}
		return reconcile.Result{Requeue: true}, nil
	}

	// Register tracker on ALL pods so tracker.Active() works everywhere
	err = r.Manager.Register(*t)
	if err != nil {
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
