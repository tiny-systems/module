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
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	perrors "github.com/tiny-systems/module/pkg/errors"
	"github.com/tiny-systems/module/pkg/utils"
	"go.opentelemetry.io/otel/trace"
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

// deliveryTimeout bounds how long a single signal's delivery can run
// before the reconcile worker gives up and requeues for retry. Long
// enough for typical handlers (HTTP requests, K8s API calls, etc.);
// short enough that a stuck handler doesn't permanently starve the
// reconcile loop.
const deliveryTimeout = 30 * time.Second

// Reconcile handles TinySignal delivery with delete-after-success semantics.
//
// Order:
//  1. Filter to this module by signal.Spec.Node (signals are leader-gated
//     and module-scoped — see RequeueAllOnLeadershipChange).
//  2. Deliver synchronously to the scheduler.
//  3. On success — delete this signal.
//  4. On permanent error — log and delete; the work has terminally
//     failed, no point retrying forever.
//  5. On transient error — leave the signal alone, requeue. The
//     controller will retry.
func (r *TinySignalReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Only leader processes signals — requeue if not leader so we don't miss signals
	if !r.IsLeader.Load() {
		l.V(1).Info("non-leader skipping signal, will requeue", "name", req.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	var signal operatorv1alpha1.TinySignal
	if err := r.Get(ctx, req.NamespacedName, &signal); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Skip if already being deleted
	if !signal.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Module-scope filter: parse the target node, not the signal's own
	// metadata.name.
	if signal.Spec.Node == "" {
		l.Error(fmt.Errorf("empty Spec.Node"), "tinysignal has no target node", "name", req.Name)
		return ctrl.Result{}, nil
	}
	m, _, err := module.ParseFullName(signal.Spec.Node)
	if err != nil {
		l.Error(err, "tinysignal has invalid Spec.Node", "name", req.Name, "node", signal.Spec.Node)
		return ctrl.Result{}, nil
	}
	if m != r.Module.GetNameSanitised() {
		return ctrl.Result{}, nil
	}

	targetPort := utils.GetPortFullName(signal.Spec.Node, signal.Spec.Port)

	// Wait for target instance to be live before delivery. Without this
	// the scheduler's own backoff would spin for 30s on every reconcile
	// of a signal whose target hasn't started yet.
	if !r.Scheduler.HasInstance(signal.Spec.Node) {
		l.Info("target instance not ready, requeuing", "node", signal.Spec.Node)
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	deliveryCtx, cancel := context.WithTimeout(context.Background(), deliveryTimeout)
	defer cancel()
	deliveryCtx = utils.WithLeader(deliveryCtx, true)

	// Inject a remote span context if the signal carries a TraceID so
	// the resulting trace stitches under the caller-supplied root.
	if signal.Spec.TraceID != "" {
		if traceIDBytes, err := hex.DecodeString(signal.Spec.TraceID); err == nil && len(traceIDBytes) == 16 {
			var tid trace.TraceID
			copy(tid[:], traceIDBytes)
			spanID := trace.SpanID{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
			sc := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    tid,
				SpanID:     spanID,
				TraceFlags: trace.FlagsSampled,
				Remote:     true,
			})
			deliveryCtx = trace.ContextWithRemoteSpanContext(deliveryCtx, sc)
		}
	}

	l.Info("delivering signal", "targetPort", targetPort)
	_, deliverErr := r.Scheduler.Handle(deliveryCtx, &runner.Msg{
		From:   runner.FromSignal,
		To:     targetPort,
		Data:   signal.Spec.Data,
		EdgeID: signal.Spec.EdgeID,
	})

	if deliverErr != nil {
		if perrors.IsPermanent(deliverErr) {
			// Terminal failure: drop the signal so we don't loop forever.
			// Note: this is a deliberate data-loss point — permanent
			// errors should be rare, and infinite retry is worse.
			l.Error(deliverErr, "permanent error during signal delivery, dropping signal",
				"targetPort", targetPort, "name", req.Name)
			if delErr := r.Delete(ctx, &signal); delErr != nil && !errors.IsNotFound(delErr) {
				l.Error(delErr, "failed to delete permanent-error signal", "name", req.Name)
				return ctrl.Result{}, delErr
			}
			return ctrl.Result{}, nil
		}
		// Transient: leave the signal alone for retry. Pod kill mid-handle
		// follows this branch — context cancel is treated as transient.
		l.Error(deliverErr, "transient error during signal delivery, leaving signal for retry",
			"targetPort", targetPort, "name", req.Name)
		return ctrl.Result{Requeue: true}, deliverErr
	}

	// Success: delete this signal.
	if err := r.Delete(ctx, &signal); err != nil && !errors.IsNotFound(err) {
		l.Error(err, "failed to delete signal after successful delivery", "name", req.Name)
		return ctrl.Result{}, err
	}
	l.Info("signal delivered and deleted", "targetPort", targetPort)
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
