package scheduler

// Per-port durability tests.
//
// These verify the recipient-side intake-via-TinySignal path:
// when a message arrives at a port marked Port.Durable=true, the scheduler
// persists a TinySignal CRD (via Manager.PersistDurableSignal) and returns
// immediately. Delivery happens asynchronously through the TinySignal
// reconciler — that's covered by the next task; here we only assert the
// persist-and-ack contract.

import (
	"context"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/state"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// recordingManager wraps fakeManager and captures PersistDurableSignal calls
// so tests can assert what was persisted.
type recordingManager struct {
	fakeManager
	mu    sync.Mutex
	calls []persistCall
}

type persistCall struct {
	name      string
	nodeName  string
	namespace string
	port      string
	data      []byte
	edgeID    string
	from      string
}

func (r *recordingManager) PersistDurableSignal(_ context.Context, name, nodeName, namespace, port string, data []byte, edgeID, from string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, persistCall{
		name:      name,
		nodeName:  nodeName,
		namespace: namespace,
		port:      port,
		data:      append([]byte(nil), data...),
		edgeID:    edgeID,
		from:      from,
	})
	return nil
}

func (r *recordingManager) recordedCalls() []persistCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]persistCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// durableSinkComponent has a single durable input port. The Handle method
// must NEVER be invoked for the durable port from the gRPC fast path —
// the scheduler should persist a TinySignal and return without dispatching.
type durableSinkComponent struct {
	name     string
	handleHits int
	mu       sync.Mutex
}

func (d *durableSinkComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: d.name}
}
func (d *durableSinkComponent) Instance() module.Component { return d }
func (d *durableSinkComponent) Ports() []module.Port {
	return []module.Port{
		{Name: "in", Configuration: map[string]any{}, Durable: true},
	}
}
func (d *durableSinkComponent) Handle(_ context.Context, _ module.Handler, port string, _ any) any {
	if port == "in" {
		d.mu.Lock()
		d.handleHits++
		d.mu.Unlock()
	}
	return nil
}

func (d *durableSinkComponent) hits() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.handleHits
}

// newDurableTestSchedule mirrors newTestSchedule but installs a recording
// manager so tests can inspect PersistDurableSignal calls.
func newDurableTestSchedule(t *testing.T, mgr *recordingManager) *Schedule {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.TinyNode{}).
		Build()

	return New(func(_ context.Context, _ *runner.Msg) (any, error) { return nil, nil }).
		SetLogger(logr.Discard()).
		SetManager(mgr).
		SetTracer(tracenoop.NewTracerProvider().Tracer("test")).
		SetMeter(metricnoop.NewMeterProvider().Meter("test")).
		SetStateFactory(state.NewMetadataFactory(fakeClient))
}

// TestDurablePort_PersistsAndReturnsEarly is the headline contract:
// a non-signal message to a Durable port persists a TinySignal and the
// scheduler returns immediately without calling Component.Handle.
func TestDurablePort_PersistsAndReturnsEarly(t *testing.T) {
	mgr := &recordingManager{}
	s := newDurableTestSchedule(t, mgr)

	cmp := &durableSinkComponent{name: "dsink"}
	if err := s.Install(cmp); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installNode(t, s, "dsink-1", "dsink")

	resp, err := s.Handle(context.Background(), &runner.Msg{
		From:   "external-sender",
		To:     "dsink-1:in",
		Data:   []byte(`{"hello":"world"}`),
		EdgeID: "edge-abc",
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if resp != nil {
		t.Errorf("expected resp=nil for durable port persist; got %v", resp)
	}

	// Persist call captured.
	calls := mgr.recordedCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 PersistDurableSignal call; got %d", len(calls))
	}
	got := calls[0]
	if got.nodeName != "dsink-1" {
		t.Errorf("nodeName = %q; want %q", got.nodeName, "dsink-1")
	}
	if got.port != "in" {
		t.Errorf("port = %q; want %q", got.port, "in")
	}
	if got.edgeID != "edge-abc" {
		t.Errorf("edgeID = %q; want %q", got.edgeID, "edge-abc")
	}
	if got.from != "external-sender" {
		t.Errorf("from = %q; want %q", got.from, "external-sender")
	}
	if string(got.data) != `{"hello":"world"}` {
		t.Errorf("data = %q; want %q", string(got.data), `{"hello":"world"}`)
	}
	if got.name == "" {
		t.Error("signal name was empty")
	}

	// Component.Handle MUST NOT have been invoked for the durable port.
	if cmp.hits() != 0 {
		t.Errorf("Component.Handle was invoked %d times for durable port; want 0", cmp.hits())
	}
}

// TestDurablePort_RetriedSendIsIdempotent verifies that two sends with the
// same payload + edgeID produce the same signal name (deterministic), so
// a retried gRPC at-least-once doesn't create duplicate work. Manager's
// real PersistDurableSignal would treat the second create as AlreadyExists
// and return nil; the fake records both, but their names are identical.
func TestDurablePort_RetriedSendIsIdempotent(t *testing.T) {
	mgr := &recordingManager{}
	s := newDurableTestSchedule(t, mgr)

	cmp := &durableSinkComponent{name: "dsink"}
	if err := s.Install(cmp); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installNode(t, s, "dsink-1", "dsink")

	for i := 0; i < 2; i++ {
		_, err := s.Handle(context.Background(), &runner.Msg{
			From:   "sender",
			To:     "dsink-1:in",
			Data:   []byte(`{"x":1}`),
			EdgeID: "edge-same",
		})
		if err != nil {
			t.Fatalf("Handle iteration %d: %v", i, err)
		}
	}

	calls := mgr.recordedCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (fake records every attempt); got %d", len(calls))
	}
	if calls[0].name != calls[1].name {
		t.Errorf("retried sends with identical inputs produced different signal names: %q vs %q",
			calls[0].name, calls[1].name)
	}
}

// TestDurablePort_DistinctEdgesProduceDistinctNames verifies that two
// senders firing identical payloads to the same target through different
// edges produce different signal names. Both signals coexist (different
// CRDs), both get delivered — that's correct fan-in semantics.
func TestDurablePort_DistinctEdgesProduceDistinctNames(t *testing.T) {
	mgr := &recordingManager{}
	s := newDurableTestSchedule(t, mgr)

	cmp := &durableSinkComponent{name: "dsink"}
	if err := s.Install(cmp); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installNode(t, s, "dsink-1", "dsink")

	for _, edgeID := range []string{"edge-from-A", "edge-from-B"} {
		_, err := s.Handle(context.Background(), &runner.Msg{
			From:   "sender",
			To:     "dsink-1:in",
			Data:   []byte(`{"shared":"payload"}`),
			EdgeID: edgeID,
		})
		if err != nil {
			t.Fatalf("Handle for edge %s: %v", edgeID, err)
		}
	}

	calls := mgr.recordedCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls; got %d", len(calls))
	}
	if calls[0].name == calls[1].name {
		t.Errorf("distinct edges produced identical signal names: %q", calls[0].name)
	}
}

// TestDurablePort_SignalDeliveryDoesNotRePersist verifies that when the
// TinySignal reconciler delivers a signal (msg.From == FromSignal), the
// scheduler does NOT re-persist — it dispatches normally so the component
// gets the message. Without this guard we'd recursively persist signals
// forever.
func TestDurablePort_SignalDeliveryDoesNotRePersist(t *testing.T) {
	mgr := &recordingManager{}
	s := newDurableTestSchedule(t, mgr)

	cmp := &durableSinkComponent{name: "dsink"}
	if err := s.Install(cmp); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installNode(t, s, "dsink-1", "dsink")

	_, err := s.Handle(context.Background(), &runner.Msg{
		From: runner.FromSignal,
		To:   "dsink-1:in",
		Data: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if calls := mgr.recordedCalls(); len(calls) != 0 {
		t.Errorf("expected 0 PersistDurableSignal calls for signal-driven delivery; got %d", len(calls))
	}
	if cmp.hits() != 1 {
		t.Errorf("expected Component.Handle to be invoked once for signal-driven delivery; got %d", cmp.hits())
	}
}

// TestDurablePort_NonDurablePortStillFastPaths verifies that ports without
// Durable=true preserve the existing blocking-I/O fast path: no persist
// call, Handle invoked synchronously.
func TestDurablePort_NonDurablePortStillFastPaths(t *testing.T) {
	mgr := &recordingManager{}
	s := newDurableTestSchedule(t, mgr)

	cmp := &passThroughComponent{name: "pass"}
	if err := s.Install(cmp); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installNode(t, s, "pass-1", "pass")

	_, err := s.Handle(context.Background(), &runner.Msg{
		From: "external",
		To:   "pass-1:in",
		Data: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if calls := mgr.recordedCalls(); len(calls) != 0 {
		t.Errorf("non-durable port should not trigger persist; got %d calls", len(calls))
	}
}
