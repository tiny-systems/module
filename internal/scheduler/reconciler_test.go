package scheduler

// Run-reconciler (Phase 2c) contract tests. The reconciler re-drives a run's
// FRONTIER — hops durably emitted but never completed — and only when the run
// has shown no progress past the staleness threshold. Completed and failed
// steps are terminal; finished runs get GC'd after retention.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/pkg/state"
)

// newReconcilerFixture returns a Schedule wired with a live exec KV and a
// capturing msgHandler, plus the KV for seeding.
func newReconcilerFixture(t *testing.T) (*Schedule, jetstream.KeyValue, *[]runner.Msg) {
	t.Helper()
	url := startDurableJS(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	kv, err := state.EnsureExecKV(context.Background(), js)
	if err != nil {
		t.Fatalf("EnsureExecKV: %v", err)
	}

	var published []runner.Msg
	s := New(func(ctx context.Context, msg *runner.Msg) (any, error) {
		published = append(published, *msg)
		return nil, nil
	}).SetLogger(logr.Discard())
	s.SetExecKV(kv)
	return s, kv, &published
}

// seedStep writes a ledger record directly into the KV using the layout the
// runner's JetStreamState produces: exec/<runID>/step/<stepKey>.
func seedStep(t *testing.T, kv jetstream.KeyValue, runID, stepKey string, rec runner.StepRecord) {
	t.Helper()
	b, _ := json.Marshal(rec)
	if _, err := kv.Put(context.Background(), "exec/"+runID+"/step/"+stepKey, b); err != nil {
		t.Fatalf("seed %s/%s: %v", runID, stepKey, err)
	}
}

func emitTo(stepKey string) runner.EmitRecord {
	return runner.EmitRecord{
		To:      "flow1." + durableModule + ".b:in",
		From:    "flow1." + durableModule + ".a:out",
		EdgeID:  "ea",
		StepKey: stepKey,
		Data:    []byte(`{"x":1}`),
	}
}

func TestRunReconciler_ReDrivesStaleFrontier(t *testing.T) {
	s, kv, published := newReconcilerFixture(t)

	// Entry step completed 10m ago and emitted SK1; SK1 never completed.
	seedStep(t, kv, "run-rec", "run-rec.root", runner.StepRecord{
		Node:        "flow1." + durableModule + ".a",
		Status:      runner.StepStatusDone,
		Emits:       []runner.EmitRecord{emitTo("SK1")},
		CompletedAt: time.Now().Add(-10 * time.Minute),
	})

	redriven, err := s.reconcileRuns(context.Background())
	if err != nil {
		t.Fatalf("reconcileRuns: %v", err)
	}
	if len(redriven) != 1 || len(*published) != 1 {
		t.Fatalf("want exactly 1 re-driven hop, got redriven=%d published=%d", len(redriven), len(*published))
	}
	got := (*published)[0]
	if got.RunID != "run-rec" || got.StepKey != "SK1" || got.To != "flow1."+durableModule+".b:in" {
		t.Fatalf("re-driven hop wrong: %+v", got)
	}
	if string(got.Data) != `{"x":1}` {
		t.Fatalf("payload must be re-published verbatim, got %s", got.Data)
	}
}

func TestRunReconciler_FreshRunLeftAlone(t *testing.T) {
	s, kv, published := newReconcilerFixture(t)

	seedStep(t, kv, "run-fresh", "run-fresh.root", runner.StepRecord{
		Status:      runner.StepStatusDone,
		Emits:       []runner.EmitRecord{emitTo("SK1")},
		CompletedAt: time.Now(), // progress just happened — hop is in flight
	})

	redriven, err := s.reconcileRuns(context.Background())
	if err != nil {
		t.Fatalf("reconcileRuns: %v", err)
	}
	if len(redriven) != 0 || len(*published) != 0 {
		t.Fatalf("fresh run must not be re-driven, got %d", len(redriven))
	}
}

func TestRunReconciler_FailedStepIsTerminal(t *testing.T) {
	s, kv, published := newReconcilerFixture(t)

	seedStep(t, kv, "run-fail", "run-fail.root", runner.StepRecord{
		Status:      runner.StepStatusDone,
		Emits:       []runner.EmitRecord{emitTo("SK1")},
		CompletedAt: time.Now().Add(-10 * time.Minute),
	})
	seedStep(t, kv, "run-fail", "SK1", runner.StepRecord{
		Status:      runner.StepStatusFailed,
		Error:       "boom",
		CompletedAt: time.Now().Add(-9 * time.Minute),
	})

	redriven, err := s.reconcileRuns(context.Background())
	if err != nil {
		t.Fatalf("reconcileRuns: %v", err)
	}
	if len(redriven) != 0 || len(*published) != 0 {
		t.Fatalf("failed step must be terminal — no re-drive, got %d", len(redriven))
	}
}

// TestRunReconciler_EndToEnd_RedriveCompletesRun closes the whole 2a+2b+2c
// loop: a run whose emitted hop vanished (stalled 10 minutes) is re-driven by
// the reconciler through the real transport, consumed by the receiver, runs
// the remaining chain, and completes.
func TestRunReconciler_EndToEnd_RedriveCompletesRun(t *testing.T) {
	url := startDurableJS(t)
	rec := newRunRecord()
	pod := newDurablePod(t, url)
	installChain(t, pod, "P", rec)
	pod.startReceiver(t)
	pod.sched.SetExecKV(pod.kv)

	// A run stalled long ago: the entry step completed and durably emitted
	// the b-hop, but the hop never produced a record (lost in a crash).
	seedStep(t, pod.kv, "run-e2e", "run-e2e.root", runner.StepRecord{
		Node:        "flow1." + durableModule + ".a",
		Status:      runner.StepStatusDone,
		Emits:       []runner.EmitRecord{emitTo("run-e2e.00000000deadbeef")},
		CompletedAt: time.Now().Add(-10 * time.Minute),
	})

	redriven, err := pod.sched.reconcileRuns(context.Background())
	if err != nil {
		t.Fatalf("reconcileRuns: %v", err)
	}
	if len(redriven) != 1 {
		t.Fatalf("want 1 re-driven hop, got %d", len(redriven))
	}

	select {
	case <-rec.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("re-driven run did not complete; hops: %v", rec.snapshot())
	}
	hops := rec.snapshot()
	if len(hops) != 2 || hops[0] != "P/step-b" || hops[1] != "P/final-c" {
		t.Fatalf("want [P/step-b P/final-c], got %v", hops)
	}
}

// TestRunReconciler_RedriveCapMarksRunFailed locks the unbounded-retry guard:
// a hop whose target never writes a step record (the pre-v0.11-SDK-module
// case) is re-driven at most maxRedrives times, then gets a terminal failed
// record — the run leaves the frontier instead of re-executing side effects
// forever.
func TestRunReconciler_RedriveCapMarksRunFailed(t *testing.T) {
	s, kv, published := newReconcilerFixture(t)

	seedStep(t, kv, "run-cap", "run-cap.root", runner.StepRecord{
		Node:        "flow1." + durableModule + ".a",
		Status:      runner.StepStatusDone,
		Emits:       []runner.EmitRecord{emitTo("SK1")},
		CompletedAt: time.Now().Add(-10 * time.Minute),
	})

	// Passes 1..maxRedrives re-drive the hop (the target "module" never
	// completes it, so no record ever appears).
	for i := 1; i <= 3; i++ {
		redriven, err := s.reconcileRuns(context.Background())
		if err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
		if len(redriven) != 1 {
			t.Fatalf("pass %d: want 1 re-drive, got %d", i, len(redriven))
		}
	}
	if got := len(*published); got != 3 {
		t.Fatalf("want exactly 3 re-publishes, got %d", got)
	}

	// Pass 4: cap reached — no re-publish; the hop gets a terminal failed
	// record instead.
	redriven, err := s.reconcileRuns(context.Background())
	if err != nil {
		t.Fatalf("cap pass: %v", err)
	}
	if len(redriven) != 0 || len(*published) != 3 {
		t.Fatalf("cap must stop re-drives: redriven=%d published=%d", len(redriven), len(*published))
	}
	entry, err := kv.Get(context.Background(), "exec/run-cap/step/SK1")
	if err != nil {
		t.Fatalf("failed record missing: %v", err)
	}
	var rec runner.StepRecord
	if err := json.Unmarshal(entry.Value(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Status != runner.StepStatusFailed || rec.Error == "" {
		t.Fatalf("want terminal failed record, got %+v", rec)
	}

	// Pass 5: the run is out of the frontier for good — nothing happens.
	redriven, err = s.reconcileRuns(context.Background())
	if err != nil || len(redriven) != 0 {
		t.Fatalf("failed run must stay terminal: %v %d", err, len(redriven))
	}
}

func TestRunReconciler_GCsCompletedRuns(t *testing.T) {
	s, kv, _ := newReconcilerFixture(t)
	s.gcAfter = time.Millisecond // everything old enough immediately

	// Complete chain: root emitted SK1, SK1 completed with no emits.
	old := time.Now().Add(-time.Hour)
	seedStep(t, kv, "run-done", "run-done.root", runner.StepRecord{
		Status: runner.StepStatusDone, Emits: []runner.EmitRecord{emitTo("SK1")}, CompletedAt: old,
	})
	seedStep(t, kv, "run-done", "SK1", runner.StepRecord{
		Status: runner.StepStatusDone, CompletedAt: old,
	})

	if _, err := s.reconcileRuns(context.Background()); err != nil {
		t.Fatalf("reconcileRuns: %v", err)
	}

	keys, err := kv.Keys(context.Background())
	if err != nil && err != jetstream.ErrNoKeysFound {
		t.Fatalf("Keys: %v", err)
	}
	for _, k := range keys {
		if k == "exec/run-done/step/run-done.root" || k == "exec/run-done/step/SK1" {
			t.Fatalf("completed run should be GC'd, key still present: %s", k)
		}
	}
}
