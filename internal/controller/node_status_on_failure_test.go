package controller

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sync/atomic"

	operatorv1alpha1 "github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	perrors "github.com/tiny-systems/module/pkg/errors"
)

// failingScheduler fails Update the way the real one does when a node names a
// component the module no longer serves.
type failingScheduler struct {
	err error
	// writesStatus mimics the real scheduler's recoverable path, which sets the
	// reason on Status itself and relies on the controller to persist it.
	writesStatus bool
}

func (f *failingScheduler) Install(module.Component) error { return nil }

func (f *failingScheduler) Update(_ context.Context, node *operatorv1alpha1.TinyNode) error {
	if f.writesStatus {
		node.Status.Status = f.err.Error()
		node.Status.Error = true
	}
	return f.err
}

func (f *failingScheduler) Handle(context.Context, *runner.Msg) (any, error) { return nil, nil }
func (f *failingScheduler) Destroy(string) error                             { return nil }
func (f *failingScheduler) HasInstance(string) bool                          { return false }

var _ scheduler.Scheduler = (*failingScheduler)(nil)

func reconcilerFor(t *testing.T, sched scheduler.Scheduler, node *operatorv1alpha1.TinyNode) (*TinyNodeReconciler, client.Client) {
	t.Helper()

	s := runtime.NewScheme()
	if err := operatorv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(node).
		WithStatusSubresource(node).
		Build()

	leader := &atomic.Bool{}
	leader.Store(true)
	return &TinyNodeReconciler{
		Client:    c,
		Scheme:    s,
		Scheduler: sched,
		IsLeader:  leader,
		Module:    module.Info{Name: "tinysystems-common-module-v0", Version: "0.1.0", SDKVersion: "0.13.0"},
	}, c
}

func nodeNaming(component string) *operatorv1alpha1.TinyNode {
	return &operatorv1alpha1.TinyNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "abc.tinysystems-common-module-v0." + component + "-1",
			Namespace:  "tinysystems",
			Finalizers: []string{nodeFinalizer},
			Generation: 3,
		},
		Spec: operatorv1alpha1.TinyNodeSpec{
			Module:    "tinysystems-common-module-v0",
			Component: component,
		},
		// The stale success this bug preserved forever.
		Status: operatorv1alpha1.TinyNodeStatus{Status: "OK"},
	}
}

// A node whose component vanished from the module — the module upgraded, the
// component was renamed, the flow was never touched — must say so in its own
// status. It reported OK indefinitely, with the dead component's ports frozen
// in from the last reconcile that happened to succeed, because the controller
// returned the scheduler's error before ever patching status. Anything reading
// the CRD (a person, an agent, the editor) saw a healthy node on a flow that
// could never route through it.
func TestVanishedComponentIsVisibleInNodeStatus(t *testing.T) {
	notRegistered := perrors.NewPermanentError(fmt.Errorf("component ask is not registered"))
	node := nodeNaming("ask")
	r, c := reconcilerFor(t, &failingScheduler{err: notRegistered}, node)

	key := types.NamespacedName{Name: node.Name, Namespace: node.Namespace}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})

	// Permanent: retrying cannot make a compiled-in registry grow the component,
	// so this must not be handed back as a reconcile error to be retried forever.
	if err != nil {
		t.Fatalf("permanent failure returned for retry: %v", err)
	}
	if res.Requeue {
		t.Error("permanent failure asked for an immediate requeue")
	}

	var got operatorv1alpha1.TinyNode
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Status.Error {
		t.Error("Status.Error is false on a node whose component does not exist")
	}
	if got.Status.Status == "OK" {
		t.Fatal("node still reports OK — the reason never reached the CRD")
	}
	if got.Status.Status != notRegistered.Error() {
		t.Errorf("status = %q, want the scheduler's reason %q", got.Status.Status, notRegistered.Error())
	}
}

// The scheduler's recoverable path writes the reason onto Status itself and
// trusts the controller to persist it. Returning before the patch threw every
// one of those away.
func TestTransientSchedulerFailureIsPersistedAndRetried(t *testing.T) {
	boom := fmt.Errorf("settings rejected: bad cron expression")
	node := nodeNaming("cron")
	r, c := reconcilerFor(t, &failingScheduler{err: boom, writesStatus: true}, node)

	key := types.NamespacedName{Name: node.Name, Namespace: node.Namespace}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	if err == nil {
		t.Error("transient failure was not returned for retry")
	}

	var got operatorv1alpha1.TinyNode
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Status != boom.Error() {
		t.Errorf("status = %q, want the scheduler's reason %q", got.Status.Status, boom.Error())
	}
	if !got.Status.Error {
		t.Error("Status.Error is false after a scheduler failure")
	}
}

// A node that reconciles cleanly must still come out OK — the fix must not
// leave a stale error behind once the cause is gone.
func TestHealthyNodeStillReportsOK(t *testing.T) {
	node := nodeNaming("cron")
	node.Status.Status = "component cron is not registered"
	node.Status.Error = true

	r, c := reconcilerFor(t, &failingScheduler{}, node)

	key := types.NamespacedName{Name: node.Name, Namespace: node.Namespace}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got operatorv1alpha1.TinyNode
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Status != "OK" || got.Status.Error {
		t.Errorf("healthy node reports %q (error=%v), want OK", got.Status.Status, got.Status.Error)
	}
	if got.Status.ObservedGeneration != node.Generation {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, node.Generation)
	}
}
