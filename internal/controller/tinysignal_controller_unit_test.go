package controller

// Unit tests for the TinySignal reconciler's delete-after-success logic.
//
// These exercise the behavior contract introduced for durable-port
// delivery: the reconciler must DELIVER first and DELETE only on success;
// transient failure must leave the signal so the next pod / next reconcile
// can retry; permanent failure must drop the signal so we don't loop forever.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	perrors "github.com/tiny-systems/module/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeScheduler implements scheduler.Scheduler for reconciler tests,
// returning a configurable result and recording delivery calls.
type fakeScheduler struct {
	mu          sync.Mutex
	hasInstance bool
	handleResp  any
	handleErr   error
	deliveries  []*runner.Msg
}

func (f *fakeScheduler) Install(_ module.Component) error                  { return nil }
func (f *fakeScheduler) Update(_ context.Context, _ *operatorv1alpha1.TinyNode) error { return nil }
func (f *fakeScheduler) Destroy(_ string) error                            { return nil }
func (f *fakeScheduler) HasInstance(_ string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasInstance
}
func (f *fakeScheduler) Handle(_ context.Context, msg *runner.Msg) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliveries = append(f.deliveries, msg)
	return f.handleResp, f.handleErr
}
func (f *fakeScheduler) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deliveries)
}

// Compile-time assertion: fakeScheduler must satisfy scheduler.Scheduler.
var _ scheduler.Scheduler = (*fakeScheduler)(nil)

// newReconcilerWithSignal builds a reconciler and a TinySignal in a fake
// client; returns both so tests can call Reconcile and then inspect state.
func newReconcilerWithSignal(t *testing.T, sched *fakeScheduler, leader bool, signalName string, spec operatorv1alpha1.TinySignalSpec, moduleName string) (*TinySignalReconciler, client.Client, *operatorv1alpha1.TinySignal) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := operatorv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	signal := &operatorv1alpha1.TinySignal{
		ObjectMeta: metav1.ObjectMeta{Name: signalName, Namespace: "default"},
		Spec:       spec,
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(signal).
		Build()

	leaderFlag := &atomic.Bool{}
	leaderFlag.Store(leader)

	r := &TinySignalReconciler{
		Client:    cl,
		Scheme:    scheme,
		Scheduler: sched,
		Module:    module.Info{Name: moduleName, Version: "0.0.1"},
		IsLeader:  leaderFlag,
	}
	return r, cl, signal
}

// signalExists reports whether the signal name still exists in the client.
func signalExists(t *testing.T, cl client.Client, name string) bool {
	t.Helper()
	got := &operatorv1alpha1.TinySignal{}
	err := cl.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, got)
	if err == nil {
		return true
	}
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return false
	}
	t.Fatalf("unexpected error checking signal: %v", err)
	return false
}

// TestReconcile_SuccessDeletesSignal — the headline contract:
// successful delivery deletes the signal as the checkpoint advance.
func TestReconcile_SuccessDeletesSignal(t *testing.T) {
	sched := &fakeScheduler{hasInstance: true, handleErr: nil}
	r, cl, sig := newReconcilerWithSignal(t, sched, true, "sig-test", operatorv1alpha1.TinySignalSpec{
		Node: "proj.testmod.comp-a",
		Port: "in",
		Data: []byte(`{"x":1}`),
	}, "testmod")

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: sig.Name, Namespace: sig.Namespace}})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if res.Requeue {
		t.Errorf("expected no requeue on success; got Requeue=true")
	}
	if sched.callCount() != 1 {
		t.Errorf("expected 1 delivery; got %d", sched.callCount())
	}
	if signalExists(t, cl, sig.Name) {
		t.Error("signal should be deleted after successful delivery")
	}
}

// TestReconcile_PermanentErrorDeletesSignal — permanent failures aren't
// retryable; deleting the signal is the right (if data-loss-y) outcome.
func TestReconcile_PermanentErrorDeletesSignal(t *testing.T) {
	sched := &fakeScheduler{
		hasInstance: true,
		handleErr:   perrors.NewPermanentError(errors.New("validation failed")),
	}
	r, cl, sig := newReconcilerWithSignal(t, sched, true, "sig-perm", operatorv1alpha1.TinySignalSpec{
		Node: "proj.testmod.comp-a",
		Port: "in",
		Data: []byte(`{}`),
	}, "testmod")

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: sig.Name, Namespace: sig.Namespace}})
	if err != nil {
		t.Fatalf("Reconcile returned error to controller-runtime; want nil so loop doesn't retry: %v", err)
	}
	if res.Requeue {
		t.Error("expected no requeue on permanent error")
	}
	if signalExists(t, cl, sig.Name) {
		t.Error("signal should be deleted on permanent error (poison-message drop)")
	}
}

// TestReconcile_TransientErrorLeavesSignal — the crash-safety property:
// any non-Permanent error leaves the signal in place so the next pod /
// next reconcile attempt can retry it.
func TestReconcile_TransientErrorLeavesSignal(t *testing.T) {
	sched := &fakeScheduler{
		hasInstance: true,
		handleErr:   errors.New("downstream timeout"),
	}
	r, cl, sig := newReconcilerWithSignal(t, sched, true, "sig-trans", operatorv1alpha1.TinySignalSpec{
		Node: "proj.testmod.comp-a",
		Port: "in",
		Data: []byte(`{}`),
	}, "testmod")

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: sig.Name, Namespace: sig.Namespace}})
	if err == nil {
		t.Error("expected reconcile to surface the transient error so controller-runtime retries")
	}
	if !res.Requeue {
		t.Error("expected requeue on transient error")
	}
	if !signalExists(t, cl, sig.Name) {
		t.Fatal("signal should NOT be deleted on transient error — it's the durability checkpoint")
	}
}

// TestReconcile_NonLeaderDoesNothing — only the leader processes signals;
// non-leaders requeue with a delay so they pick the work up if/when
// promoted.
func TestReconcile_NonLeaderDoesNothing(t *testing.T) {
	sched := &fakeScheduler{hasInstance: true}
	r, cl, sig := newReconcilerWithSignal(t, sched, false /* not leader */, "sig-foll", operatorv1alpha1.TinySignalSpec{
		Node: "proj.testmod.comp-a",
		Port: "in",
	}, "testmod")

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: sig.Name, Namespace: sig.Namespace}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 on non-leader skip")
	}
	if sched.callCount() != 0 {
		t.Error("non-leader must not deliver")
	}
	if !signalExists(t, cl, sig.Name) {
		t.Error("non-leader must not delete signals")
	}
}

// TestReconcile_WrongModuleSkips — multi-module clusters share the
// TinySignal CRD; each module's reconciler must filter to its own
// signals via Spec.Node module name and ignore the rest.
func TestReconcile_WrongModuleSkips(t *testing.T) {
	sched := &fakeScheduler{hasInstance: true}
	r, cl, sig := newReconcilerWithSignal(t, sched, true, "sig-other", operatorv1alpha1.TinySignalSpec{
		Node: "proj.othermod.comp-x",
		Port: "in",
	}, "testmod" /* my module is testmod, signal is for othermod */)

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: sig.Name, Namespace: sig.Namespace}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Requeue {
		t.Error("wrong-module signal should not cause requeue")
	}
	if sched.callCount() != 0 {
		t.Error("wrong-module signal should not be delivered")
	}
	if !signalExists(t, cl, sig.Name) {
		t.Error("wrong-module signal must not be deleted by us — it belongs to another module")
	}
}

// TestReconcile_TargetInstanceNotReadyRequeues — the signal arrives but
// the target node's runner isn't live yet (pod just started, not all
// nodes resumed). Requeue with a short delay; don't fail.
func TestReconcile_TargetInstanceNotReadyRequeues(t *testing.T) {
	sched := &fakeScheduler{hasInstance: false}
	r, cl, sig := newReconcilerWithSignal(t, sched, true, "sig-not-ready", operatorv1alpha1.TinySignalSpec{
		Node: "proj.testmod.comp-a",
		Port: "in",
	}, "testmod")

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: sig.Name, Namespace: sig.Namespace}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Error("expected RequeueAfter > 0 when target not ready")
	}
	if sched.callCount() != 0 {
		t.Error("must not deliver when target instance not ready")
	}
	if !signalExists(t, cl, sig.Name) {
		t.Error("signal must not be deleted when target wasn't reachable yet")
	}
}

// TestReconcile_ForwardsEdgeIDToScheduler — the signal carries Spec.EdgeID
// (the parent edge that produced this work); the reconciler must pass it
// to scheduler.Handle so subsequent durable-port persists hash with the
// same edgeID and dedup correctly.
func TestReconcile_ForwardsEdgeIDToScheduler(t *testing.T) {
	sched := &fakeScheduler{hasInstance: true}
	r, _, sig := newReconcilerWithSignal(t, sched, true, "sig-edge", operatorv1alpha1.TinySignalSpec{
		Node:   "proj.testmod.comp-a",
		Port:   "in",
		EdgeID: "edge-xyz",
	}, "testmod")

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: sig.Name, Namespace: sig.Namespace}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if sched.callCount() != 1 {
		t.Fatalf("expected 1 delivery; got %d", sched.callCount())
	}
	if got := sched.deliveries[0].EdgeID; got != "edge-xyz" {
		t.Errorf("EdgeID forwarded to scheduler.Handle = %q; want %q", got, "edge-xyz")
	}
	if got := sched.deliveries[0].From; got != runner.FromSignal {
		t.Errorf("From forwarded to scheduler.Handle = %q; want %q", got, runner.FromSignal)
	}
}
