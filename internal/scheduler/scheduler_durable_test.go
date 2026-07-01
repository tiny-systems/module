package scheduler

// Durable-execution (Phase 2a) contract tests.
//
// These lock in the async, ack-on-emit dispatch path that durable runs use:
//
//  1. Non-blocking entry — Handle on the entry node returns once its emit is
//     durably stored, NOT when the downstream chain completes.
//  2. Cross-pod migration — hops ride the JetStream work queue, so a chain
//     started on pod A completes on pod B even when pod A's NATS connection
//     is severed right after the first emit (sender pod death).
//  3. Idempotent publish — two publishes with the same StepKey collapse to
//     one stored message (Nats-Msg-Id dedup), the guard that makes a
//     redelivered handler's re-emit harmless.
//
// The classic blocking-I/O semantics are locked by scheduler_blocking_io_test.go
// and must keep passing unchanged — durable mode is opt-in per node.

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/transport"
	"github.com/tiny-systems/module/module"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const durableModule = "testmod"

// startDurableJS boots an embedded nats-server with JetStream and ensures the
// edge stream, returning the client URL.
func startDurableJS(t *testing.T) string {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = -1
	opts.JetStream = true
	opts.StoreDir = filepath.Join(t.TempDir(), "js")
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(2 * time.Second) {
		t.Fatal("nats/JS not ready")
	}
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	if err := transport.EnsureEdgeStream(context.Background(), js); err != nil {
		t.Fatalf("EnsureEdgeStream: %v", err)
	}
	return srv.ClientURL()
}

// durablePod bundles one "pod": its own NATS connection, transport, and
// scheduler whose router mirrors cli/run.go — durable hops (RunID set) go
// through the stream; everything else routes back into the local scheduler.
type durablePod struct {
	nc    *nats.Conn
	tr    *transport.JetStream
	sched *Schedule
}

func newDurablePod(t *testing.T, url string) *durablePod {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("pod connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("pod jetstream.New: %v", err)
	}
	p := &durablePod{nc: nc, tr: transport.NewJetStream(js, nc, durableModule, logr.Discard())}
	p.sched = New(func(ctx context.Context, msg *runner.Msg) (any, error) {
		if msg.RunID != "" {
			return p.tr.Handler(ctx, msg)
		}
		return p.sched.Handle(ctx, msg)
	}).
		SetLogger(logr.Discard()).
		SetManager(fakeManager{}).
		SetTracer(tracenoop.NewTracerProvider().Tracer("test")).
		SetMeter(metricnoop.NewMeterProvider().Meter("test"))
	return p
}

// startReceiver consumes this pod's work-queue deliveries into its scheduler.
func (p *durablePod) startReceiver(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = p.tr.StartReceiver(ctx, func(hctx context.Context, msg *runner.Msg) (any, error) {
			return p.sched.Handle(hctx, msg)
		})
	}()
	// Give CreateOrUpdateConsumer + Consume a moment to be live.
	time.Sleep(300 * time.Millisecond)
}

// runRecord collects which pod executed which node, and completion.
type runRecord struct {
	mu   sync.Mutex
	hops []string // "pod/node"
	once sync.Once
	done chan struct{}
}

func newRunRecord() *runRecord { return &runRecord{done: make(chan struct{})} }

func (r *runRecord) add(pod, node string) {
	r.mu.Lock()
	r.hops = append(r.hops, pod+"/"+node)
	r.mu.Unlock()
}

func (r *runRecord) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hops...)
}

func (r *runRecord) complete() { r.once.Do(func() { close(r.done) }) }

// recorderComponent records (pod, name) on `in`, then forwards to `out` —
// or, when terminal, sleeps and marks the run complete.
type recorderComponent struct {
	name     string
	pod      string
	rec      *runRecord
	terminal bool
	sleep    time.Duration
}

func (p *recorderComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: p.name}
}
func (p *recorderComponent) Instance() module.Component {
	cp := *p
	return &cp
}
func (p *recorderComponent) Ports() []module.Port {
	return []module.Port{
		{Name: "in", Configuration: map[string]any{}},
		{Name: "out", Source: true, Configuration: map[string]any{}},
	}
}
func (p *recorderComponent) Handle(ctx context.Context, output module.Handler, port string, msg any) module.Result {
	if port != "in" {
		return module.Result{}
	}
	if p.sleep > 0 {
		time.Sleep(p.sleep)
	}
	p.rec.add(p.pod, p.name)
	if p.terminal {
		p.rec.complete()
		return module.Ok(nil)
	}
	return output(ctx, "out", msg)
}

// installDurableNode registers a durable-labeled TinyNode on the schedule.
func installDurableNode(t *testing.T, s *Schedule, name, component string, edges ...v1alpha1.TinyNodeEdge) {
	t.Helper()
	node := &v1alpha1.TinyNode{}
	node.Name = name
	node.Namespace = "default"
	node.Labels = map[string]string{v1alpha1.ExecutionModeLabel: v1alpha1.ExecutionModeDurable}
	node.Spec.Component = component
	node.Spec.Edges = edges
	if err := s.Update(context.Background(), node); err != nil {
		t.Fatalf("Update %s: %v", name, err)
	}
}

// installChain installs the durable a→b→c chain on a pod with pod-tagged
// recorder components.
func installChain(t *testing.T, p *durablePod, pod string, rec *runRecord) {
	t.Helper()
	for _, c := range []*recorderComponent{
		{name: "step-a", pod: pod, rec: rec},
		{name: "step-b", pod: pod, rec: rec},
		{name: "final-c", pod: pod, rec: rec, terminal: true, sleep: 200 * time.Millisecond},
	} {
		if err := p.sched.Install(c); err != nil {
			t.Fatalf("Install %s: %v", c.name, err)
		}
	}
	installDurableNode(t, p.sched, "flow1."+durableModule+".a", "step-a", v1alpha1.TinyNodeEdge{
		ID: "ea", Port: "out", To: "flow1." + durableModule + ".b:in",
	})
	installDurableNode(t, p.sched, "flow1."+durableModule+".b", "step-b", v1alpha1.TinyNodeEdge{
		ID: "eb", Port: "out", To: "flow1." + durableModule + ".c:in",
	})
	installDurableNode(t, p.sched, "flow1."+durableModule+".c", "final-c")
}

// TestDurable_RunMigratesAcrossPodsAndSurvivesSenderDeath is the Phase 2a
// flagship: a durable chain kicked on pod A completes on pod B after pod A's
// connection is severed, and the entry Handle never blocked on the chain.
func TestDurable_RunMigratesAcrossPodsAndSurvivesSenderDeath(t *testing.T) {
	url := startDurableJS(t)
	rec := newRunRecord()

	podA := newDurablePod(t, url) // entry pod — dies right after the kick
	podB := newDurablePod(t, url) // survivor — the only receiver
	installChain(t, podA, "A", rec)
	installChain(t, podB, "B", rec)
	podB.startReceiver(t)

	// Kick the entry on pod A. The durable node mints a run; the emit is
	// durably published; Handle returns without waiting for b/c.
	start := time.Now()
	if _, err := podA.sched.Handle(context.Background(), &runner.Msg{
		From: runner.FromSignal,
		To:   "flow1." + durableModule + ".a:in",
		Data: []byte(`{}`),
	}); err != nil {
		t.Fatalf("entry Handle: %v", err)
	}
	entryLatency := time.Since(start)

	// Non-blocking: the entry returned before the chain finished (c alone
	// sleeps 200ms on pod B).
	if got := rec.snapshot(); len(got) >= 3 {
		t.Fatalf("entry Handle blocked until chain completion: hops at return %v", got)
	}

	// Sender pod death immediately after the first durable emit.
	podA.nc.Close()

	select {
	case <-rec.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("run did not complete after sender pod death; hops: %v", rec.snapshot())
	}

	hops := rec.snapshot()
	if len(hops) != 3 {
		t.Fatalf("want exactly 3 hops (no loss, no duplication), got %v", hops)
	}
	if hops[0] != "A/step-a" {
		t.Fatalf("entry hop should run on pod A, got %v", hops)
	}
	for _, h := range hops[1:] {
		if h != "B/step-b" && h != "B/final-c" {
			t.Fatalf("post-death hops must run on pod B, got %v", hops)
		}
	}
	t.Logf("entry returned in %s; chain completed on pod B: %v", entryLatency, hops)
}

// TestDurable_PublishDedupsOnStepKey locks the idempotency guard: two durable
// publishes carrying the same StepKey store exactly one message.
func TestDurable_PublishDedupsOnStepKey(t *testing.T) {
	url := startDurableJS(t)
	pod := newDurablePod(t, url) // no receiver — messages accumulate

	msg := &runner.Msg{
		To:      "flow1." + durableModule + ".b:in",
		From:    "flow1." + durableModule + ".a:out",
		EdgeID:  "ea",
		Data:    []byte(`{}`),
		RunID:   "run-dedup",
		StepKey: "run-dedup.0000000000000001",
	}
	for i := 0; i < 2; i++ {
		if _, err := pod.tr.Handler(context.Background(), msg); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	js, err := jetstream.New(pod.nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	stream, err := js.Stream(context.Background(), transport.EdgeStreamName)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	info, err := stream.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("want 1 stored message after duplicate publish, got %d", info.State.Msgs)
	}
}
