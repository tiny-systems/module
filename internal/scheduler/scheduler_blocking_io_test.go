package scheduler

// Blocking-I/O contract tests.
//
// These tests lock in the runtime's current message-routing semantics so
// future changes (notably per-port durability and TinySignal-based delivery)
// can be verified to preserve the fast-path guarantees:
//
//  1. Single-hop blocking — Handle on node A blocks until A's downstream
//     handler returns.
//  2. Multi-hop chain blocking — Handle on node A blocks until the entire
//     A→B→C chain completes.
//  3. Error propagation — an error returned by any node in the chain
//     surfaces back to the original caller.
//
// They use the in-process Schedule from scheduler.go and the test
// helpers in scheduler_test.go (newTestSchedule, fakeManager, etc.).
//
// Convention: when a test wants the scheduler's outside-handler to route
// messages back to the same scheduler (so multi-node chains work in
// process), it overrides msgHandler after New() — see selfRoutingSchedule.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	perrors "github.com/tiny-systems/module/pkg/errors"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// selfRoutingSchedule constructs a Schedule whose outside-handler loops
// back to its own Handle. This makes multi-node chains work end-to-end
// in a single in-process test.
func selfRoutingSchedule(t *testing.T) *Schedule {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.TinyNode{}).
		Build()

	var s *Schedule
	s = New(func(ctx context.Context, msg *runner.Msg) (any, error) {
		return s.Handle(ctx, msg)
	}).
		SetLogger(logr.Discard()).
		SetManager(fakeManager{}).
		SetTracer(tracenoop.NewTracerProvider().Tracer("test")).
		SetMeter(metricnoop.NewMeterProvider().Meter("test"))
	_ = fakeClient // reserved for state-using tests
	return s
}

// installNode adds a TinyNode to the schedule with the given name, component
// and edges. Returns the constructed node so tests can mutate it further.
func installNode(t *testing.T, s *Schedule, name, component string, edges ...v1alpha1.TinyNodeEdge) *v1alpha1.TinyNode {
	t.Helper()
	node := &v1alpha1.TinyNode{}
	node.Name = name
	node.Namespace = "default"
	node.Spec.Component = component
	node.Spec.Edges = edges
	if err := s.Update(context.Background(), node); err != nil {
		t.Fatalf("Update %s: %v", name, err)
	}
	return node
}

// passThroughComponent forwards any input received on `in` to `out`.
// Tests use it as a chain link.
type passThroughComponent struct {
	name string
}

func (p *passThroughComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: p.name}
}
func (p *passThroughComponent) Instance() module.Component { return &passThroughComponent{name: p.name} }
func (p *passThroughComponent) Ports() []module.Port {
	return []module.Port{
		{Name: "in", Configuration: map[string]any{}},
		{Name: "out", Source: true, Configuration: map[string]any{}},
	}
}
func (p *passThroughComponent) Handle(ctx context.Context, output module.Handler, port string, msg any) module.Result {
	if port == "in" {
		return output(ctx, "out", msg)
	}
	return module.Result{}
}

// blockingSinkComponent receives on `in` and:
//   - signals to entered channel that it has been entered,
//   - waits on release channel before returning,
//   - returns whatever returnValue is set to (typically a string or error).
type blockingSinkComponent struct {
	name        string
	entered     chan struct{}
	release     chan struct{}
	returnValue any
	calls       atomic.Int32
}

func (b *blockingSinkComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: b.name}
}
func (b *blockingSinkComponent) Instance() module.Component { return b }
func (b *blockingSinkComponent) Ports() []module.Port {
	return []module.Port{{Name: "in", Configuration: map[string]any{}}}
}
func (b *blockingSinkComponent) Handle(_ context.Context, _ module.Handler, port string, _ any) module.Result {
	if port != "in" {
		return module.Result{}
	}
	b.calls.Add(1)
	if b.entered != nil {
		select {
		case b.entered <- struct{}{}:
		default:
		}
	}
	if b.release != nil {
		<-b.release
	}
	if err, ok := b.returnValue.(error); ok {
		return module.Fail(err)
	}
	return module.Ok(b.returnValue)
}

// errorSinkComponent receives on `in` and returns the configured error.
type errorSinkComponent struct {
	name string
	err  error
}

func (e *errorSinkComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: e.name}
}
func (e *errorSinkComponent) Instance() module.Component { return e }
func (e *errorSinkComponent) Ports() []module.Port {
	return []module.Port{{Name: "in", Configuration: map[string]any{}}}
}
func (e *errorSinkComponent) Handle(_ context.Context, _ module.Handler, port string, _ any) module.Result {
	if port != "in" {
		return module.Result{}
	}
	return module.Fail(e.err)
}

// transientErrorSinkComponent fails the first N calls then returns nil,
// exercising the runtime's transient-retry path for errors.
type transientErrorSinkComponent struct {
	name        string
	failsBefore int32
	retryErr    error
	calls       atomic.Int32
}

func (t *transientErrorSinkComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: t.name}
}
func (t *transientErrorSinkComponent) Instance() module.Component { return t }
func (t *transientErrorSinkComponent) Ports() []module.Port {
	return []module.Port{{Name: "in", Configuration: map[string]any{}}}
}
func (t *transientErrorSinkComponent) Handle(_ context.Context, _ module.Handler, port string, _ any) module.Result {
	if port != "in" {
		return module.Result{}
	}
	n := t.calls.Add(1)
	if n <= t.failsBefore {
		return module.Fail(t.retryErr)
	}
	return module.Result{}
}

// edge constructs a TinyNodeEdge with a deterministic ID.
func edge(fromPort, toFullPort string) v1alpha1.TinyNodeEdge {
	return v1alpha1.TinyNodeEdge{
		ID:     "edge-" + fromPort + "-" + strings.ReplaceAll(toFullPort, ":", "-"),
		Port:   fromPort,
		To:     toFullPort,
		FlowID: "test-flow",
	}
}

// TestBlockingIO_SingleHop_HandlerBlocksUntilReturn proves that
// scheduler.Handle on the chain-entry node blocks until the downstream
// handler returns. This is the core of the synchronous-fast-path contract.
func TestBlockingIO_SingleHop_HandlerBlocksUntilReturn(t *testing.T) {
	s := selfRoutingSchedule(t)

	sink := &blockingSinkComponent{
		name:        "sink",
		entered:     make(chan struct{}, 1),
		release:     make(chan struct{}),
		returnValue: nil,
	}
	if err := s.Install(sink); err != nil {
		t.Fatalf("Install sink: %v", err)
	}
	if err := s.Install(&passThroughComponent{name: "pass"}); err != nil {
		t.Fatalf("Install pass: %v", err)
	}

	installNode(t, s, "sink-1", "sink")
	installNode(t, s, "pass-1", "pass", edge("out", "sink-1:in"))

	done := make(chan struct{})
	go func() {
		_, _ = s.Handle(context.Background(), &runner.Msg{
			From: runner.FromSignal,
			To:   "pass-1:in",
			Data: []byte(`{}`),
		})
		close(done)
	}()

	// Wait until sink is entered.
	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("sink was never entered — chain did not flow")
	}

	// Verify the caller is still blocked.
	select {
	case <-done:
		t.Fatal("Handle returned before sink released — blocking-I/O contract violated")
	case <-time.After(50 * time.Millisecond):
		// expected
	}

	// Release the sink and assert the caller now completes.
	close(sink.release)
	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("Handle did not return after sink released")
	}
}

// TestBlockingIO_MultiHopChain proves that synchronous blocking holds
// across multiple hops: A→B→C, fire to A, A is blocked until C returns.
func TestBlockingIO_MultiHopChain(t *testing.T) {
	s := selfRoutingSchedule(t)

	sink := &blockingSinkComponent{
		name:    "sink",
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	if err := s.Install(sink); err != nil {
		t.Fatalf("Install sink: %v", err)
	}
	if err := s.Install(&passThroughComponent{name: "pass"}); err != nil {
		t.Fatalf("Install pass: %v", err)
	}

	// chain: a → b → sink
	installNode(t, s, "sink-1", "sink")
	installNode(t, s, "b-1", "pass", edge("out", "sink-1:in"))
	installNode(t, s, "a-1", "pass", edge("out", "b-1:in"))

	done := make(chan struct{})
	go func() {
		_, _ = s.Handle(context.Background(), &runner.Msg{
			From: runner.FromSignal,
			To:   "a-1:in",
			Data: []byte(`{}`),
		})
		close(done)
	}()

	// Sink should be reached through both passthroughs.
	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("sink never entered — multi-hop chain did not flow")
	}

	// Caller must still be blocked.
	select {
	case <-done:
		t.Fatal("Handle returned mid-chain — blocking-I/O contract violated across hops")
	case <-time.After(50 * time.Millisecond):
	}

	close(sink.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle did not return after sink released")
	}
}

// TestBlockingIO_PermanentErrorPropagatesAcrossChain proves that an error
// wrapped with perrors.PermanentError surfaces back to the original caller
// through every passthrough hop without retry.
//
// CONTRACT: plain (non-Permanent) errors returned by a handler are treated
// as TRANSIENT and infinitely retried by sendToEdgeWithRetry. Components
// that want to fail-fast must wrap with perrors.NewPermanentError. This
// test exercises the permanent-error path; the transient-retry path is
// covered by TestBlockingIO_TransientErrorIsRetried.
func TestBlockingIO_PermanentErrorPropagatesAcrossChain(t *testing.T) {
	s := selfRoutingSchedule(t)

	wantErr := errors.New("downstream failure")
	sink := &errorSinkComponent{name: "permsink", err: perrors.NewPermanentError(wantErr)}
	if err := s.Install(sink); err != nil {
		t.Fatalf("Install permsink: %v", err)
	}
	if err := s.Install(&passThroughComponent{name: "pass"}); err != nil {
		t.Fatalf("Install pass: %v", err)
	}

	installNode(t, s, "sink-1", "permsink")
	installNode(t, s, "b-1", "pass", edge("out", "sink-1:in"))
	installNode(t, s, "a-1", "pass", edge("out", "b-1:in"))

	resp, err := s.Handle(context.Background(), &runner.Msg{
		From: runner.FromSignal,
		To:   "a-1:in",
		Data: []byte(`{}`),
	})

	if err == nil {
		t.Fatalf("expected a permanent error to propagate; got resp=%v err=nil", resp)
	}
	if !strings.Contains(err.Error(), "downstream failure") {
		t.Errorf("error did not propagate verbatim through chain: got %q, want substring %q",
			err.Error(), "downstream failure")
	}
}

// TestBlockingIO_TransientErrorNoLongerRetried proves the inverse of
// the original contract: as of 2026-05-20 the runner no longer retries
// transient errors at all. Edge delivery is single-shot — a non-
// Permanent error returns to the caller on the first failed attempt.
// Components that want resilience handle it internally; flows that
// want retry semantics across edges wire an explicit retry component.
//
// PermanentError still propagates (see
// TestBlockingIO_PermanentErrorPropagatesAcrossChain) for callers
// that want to inspect the error kind — but the framework treats both
// kinds identically at delivery time.
func TestBlockingIO_TransientErrorNoLongerRetried(t *testing.T) {
	s := selfRoutingSchedule(t)

	transient := &transientErrorSinkComponent{
		name:        "transient",
		failsBefore: 100, // arbitrary; we never reach the success branch now
		retryErr:    errors.New("flaky"),
	}
	if err := s.Install(transient); err != nil {
		t.Fatalf("Install transient: %v", err)
	}
	if err := s.Install(&passThroughComponent{name: "pass"}); err != nil {
		t.Fatalf("Install pass: %v", err)
	}

	installNode(t, s, "sink-1", "transient")
	installNode(t, s, "p-1", "pass", edge("out", "sink-1:in"))

	_, err := s.Handle(context.Background(), &runner.Msg{
		From: runner.FromSignal,
		To:   "p-1:in",
		Data: []byte(`{}`),
	})

	if err == nil {
		t.Fatalf("expected single-shot failure; got success without retries")
	}
	if got := transient.calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 attempt (no retry), got %d", got)
	}
}

// TestBlockingIO_ConcurrentChainsIsolate proves that two concurrent
// invocations of the same chain don't deadlock or share state. Each one
// blocks independently until its own sink-release fires.
func TestBlockingIO_ConcurrentChainsIsolate(t *testing.T) {
	s := selfRoutingSchedule(t)

	sink := &blockingSinkComponent{
		name:    "csink",
		entered: make(chan struct{}, 8),
		release: make(chan struct{}),
	}
	if err := s.Install(sink); err != nil {
		t.Fatalf("Install csink: %v", err)
	}
	if err := s.Install(&passThroughComponent{name: "pass"}); err != nil {
		t.Fatalf("Install pass: %v", err)
	}
	installNode(t, s, "sink-1", "csink")
	installNode(t, s, "p-1", "pass", edge("out", "sink-1:in"))

	const N = 4
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, _ = s.Handle(context.Background(), &runner.Msg{
				From: runner.FromSignal,
				To:   "p-1:in",
				Data: []byte(`{}`),
			})
		}()
	}

	// Wait until at least one sink invocation has started.
	for waited := 0; sink.calls.Load() < 1 && waited < 20; waited++ {
		time.Sleep(50 * time.Millisecond)
	}
	if sink.calls.Load() < 1 {
		t.Fatal("no sink invocations after 1s — chains did not start")
	}

	// Release the sink for all goroutines.
	close(sink.release)

	doneAll := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneAll)
	}()
	select {
	case <-doneAll:
	case <-time.After(5 * time.Second):
		t.Fatalf("not all chains completed; sink calls=%d", sink.calls.Load())
	}
}
