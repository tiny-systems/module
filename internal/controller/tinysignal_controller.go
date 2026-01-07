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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sync"
	"sync/atomic"
	"time"

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
	IsLeader  *atomic.Bool
	//
	mu               *sync.Mutex
	runningProcesses map[types.NamespacedName]context.CancelFunc

	// This is used to determine if a process needs to be restarted due to a spec change.
	processedNonce map[types.NamespacedName]string
}

const signalFinalizer = "io.tinysystems/signal-finalizer"

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
	// tiny signal names after name of node it's signaling to

	if !r.IsLeader.Load() {
		return reconcile.Result{}, nil
	}
	// only leaders process signals

	// to avoid making many queries to Kubernetes API we check name itself against current module
	m, _, err := module.ParseFullName(req.Name)
	if err != nil {
		l.Error(err, "tinysignal has invalid name", "name", req.Name)
		return reconcile.Result{}, err
	}

	if m != r.Module.GetNameSanitised() {
		return reconcile.Result{}, nil
	}

	var signal operatorv1alpha1.TinySignal

	if err := r.Get(ctx, req.NamespacedName, &signal); err != nil {
		if errors.IsNotFound(err) {
			// The resource was not found, likely deleted. The delete logic below in the
			// finalizer section will have already run, or there's nothing for us to do.
			l.Info("tinySignal not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !signal.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is being deleted.
		if controllerutil.ContainsFinalizer(&signal, signalFinalizer) {
			l.Info("tinySignal is being deleted, stopping associated process.")

			// Look up the process's cancel function in our map.
			if cancel, ok := r.runningProcesses[req.NamespacedName]; ok {
				// Call the cancel function to signal the goroutine to stop.
				cancel()
				// Remove the entries from our maps to clean up.
				delete(r.runningProcesses, req.NamespacedName)
				delete(r.processedNonce, req.NamespacedName)
				l.Info("process stopped successfully.")
			}

			// Now that our cleanup is done, remove the finalizer.
			// This allows the Kubernetes garbage collector to actually delete the resource.
			controllerutil.RemoveFinalizer(&signal, signalFinalizer)
			if err := r.Update(ctx, &signal); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the signal is being deleted.
		return ctrl.Result{}, nil
	}

	// --- Add Finalizer if it doesn't exist ---
	if !controllerutil.ContainsFinalizer(&signal, signalFinalizer) {
		l.Info("adding finalizer to signal.")
		controllerutil.AddFinalizer(&signal, signalFinalizer)
		if err := r.Update(ctx, &signal); err != nil {
			return ctrl.Result{}, err
		}
		// Return here - the Update will trigger another reconcile which will handle the goroutine
		return ctrl.Result{}, nil
	}

	_, isRunning := r.runningProcesses[req.NamespacedName]
	lastProcessedNonce := r.processedNonce[req.NamespacedName]
	currentNonce := signal.Annotations[operatorv1alpha1.SignalNonceAnnotation]

	specHasChanged := currentNonce != lastProcessedNonce

	// Determine if we need to process this signal
	needsProcessing := lastProcessedNonce == "" || specHasChanged

	l.Info("signal controller: reconcile state",
		"isRunning", isRunning,
		"specHasChanged", specHasChanged,
		"needsProcessing", needsProcessing,
		"lastProcessedNonce", lastProcessedNonce,
		"currentNonce", currentNonce,
	)

	// If already running and spec changed, cancel the previous in-flight request
	// Don't cancel if spec hasn't changed - this would kill long-running handlers
	if isRunning && specHasChanged {
		l.Info("signal controller: spec changed, cancelling previous in-flight request",
			"lastProcessedNonce", lastProcessedNonce,
			"currentNonce", currentNonce,
		)
		if cancel, ok := r.runningProcesses[req.NamespacedName]; ok {
			cancel() // Signal the old process to stop.
		}
		delete(r.runningProcesses, req.NamespacedName)
		isRunning = false // Allow new process to start
	}

	// Only start a new process if not running and needs processing
	if !isRunning && needsProcessing {

		// Create a new context that we can cancel later.
		processCtx, cancel := context.WithCancel(utils.WithLeader(context.Background(), r.IsLeader.Load()))

		r.runningProcesses[req.NamespacedName] = cancel
		r.processedNonce[req.NamespacedName] = signal.Annotations[operatorv1alpha1.SignalNonceAnnotation]

		go func(ctx context.Context, spec operatorv1alpha1.TinySignalSpec, nonce string) {
			targetPort := utils.GetPortFullName(spec.Node, spec.Port)

			l.Info("signal controller: process goroutine started",
				"node", spec.Node,
				"port", spec.Port,
				"targetPort", targetPort,
				"nonce", nonce,
				"dataSize", len(spec.Data),
			)

			// Wait for target instance before sending
			waitStart := time.Now()
			waitAttempts := 0
			for !r.Scheduler.HasInstance(spec.Node) {
				waitAttempts++
				if waitAttempts%10 == 0 {
					l.Info("signal controller: still waiting for instance",
						"node", spec.Node,
						"waitAttempts", waitAttempts,
						"waitDuration", time.Since(waitStart).String(),
					)
				}
				select {
				case <-ctx.Done():
					l.Info("signal controller: context cancelled while waiting for instance",
						"node", spec.Node,
						"waitAttempts", waitAttempts,
						"waitDuration", time.Since(waitStart).String(),
						"ctxErr", ctx.Err(),
					)
					return
				case <-time.After(500 * time.Millisecond):
				}
			}
			l.Info("signal controller: instance found, proceeding to send signal",
				"node", spec.Node,
				"waitAttempts", waitAttempts,
				"waitDuration", time.Since(waitStart).String(),
			)

			// Exponential backoff configuration for retries on failure.
			initialBackoff := 2 * time.Second
			maxBackoff := 30 * time.Second
			maxAttempts := 10 // Stop retrying after this many consecutive failures
			currentBackoff := initialBackoff
			attempt := 0

			for {
				// Check if we should stop before attempting
				if ctx.Err() != nil {
					l.Info("signal controller: context cancelled, shutting down",
						"targetPort", targetPort,
						"attempt", attempt,
					)
					return
				}

				attempt++

				l.Info("signal controller: sending signal to scheduler",
					"targetPort", targetPort,
					"attempt", attempt,
					"ctxErrBeforeHandle", ctx.Err(),
				)

				handleStart := time.Now()

				// Execute the actual business logic.
				res, err := r.Scheduler.Handle(ctx, &runner.Msg{
					From:  runner.FromSignal,
					To:    targetPort,
					Data:  spec.Data,
					Nonce: nonce,
				})

				handleDuration := time.Since(handleStart)

				if err != nil {
					l.Info("signal controller: handle returned error",
						"targetPort", targetPort,
						"attempt", attempt,
						"error", err.Error(),
						"handleDuration", handleDuration.String(),
						"ctxErrAfterHandle", ctx.Err(),
					)

					// Only stop if our process context was cancelled, not for internal context errors
					if ctx.Err() != nil {
						l.Info("signal controller: context cancelled after handle, shutting down",
							"targetPort", targetPort,
							"attempt", attempt,
							"handleDuration", handleDuration.String(),
						)
						return
					}

					l.Error(err, "signal controller: scheduler handle failed, will retry with backoff",
						"targetPort", targetPort,
						"attempt", attempt,
						"nextRetryIn", currentBackoff,
						"handleDuration", handleDuration.String(),
					)

					// Check if we've exceeded max attempts
					if attempt >= maxAttempts {
						l.Error(err, "signal controller: max retry attempts reached, giving up. Port may not exist on target node.",
							"targetPort", targetPort,
							"maxAttempts", maxAttempts,
						)
						return
					}

					// Wait for the backoff period
					time.Sleep(currentBackoff)
					currentBackoff *= 2
					if currentBackoff > maxBackoff {
						currentBackoff = maxBackoff
					}
					continue
				}

				l.Info("signal controller: scheduler handle succeeded",
					"targetPort", targetPort,
					"attempt", attempt,
					"hasResponse", res != nil,
					"handleDuration", handleDuration.String(),
				)

				// Signal sent successfully - fire once, then exit
				l.Info("signal controller: signal delivered successfully, exiting",
					"targetPort", targetPort,
				)

				// Clean up from runningProcesses map
				r.mu.Lock()
				delete(r.runningProcesses, req.NamespacedName)
				r.mu.Unlock()

				return
			}
		}(processCtx, signal.Spec, signal.Annotations[operatorv1alpha1.SignalNonceAnnotation])
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TinySignalReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.mu = &sync.Mutex{}
	r.processedNonce = make(map[types.NamespacedName]string)
	r.runningProcesses = make(map[types.NamespacedName]context.CancelFunc)

	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.TinySignal{}).
		Complete(r)
}
