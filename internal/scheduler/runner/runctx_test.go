package runner

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// Step-key derivation must be deterministic across a redelivery: the broker
// dedupes on Nats-Msg-Id, so a re-executed handler must re-emit byte-identical
// keys no matter how the concurrent per-edge fan-out interleaves.
func TestNextStepKey_DeterministicAcrossConcurrentFanOut(t *testing.T) {
	edges := []string{"e1", "e2", "e3", "e4", "e5"}

	// First execution: concurrent fan-out (mirrors outputHandler's errgroup).
	first := NewRunInfo("run-1", "run-1.root")
	got := make(map[string]string, len(edges))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, e := range edges {
		wg.Add(1)
		go func(edgeID string) {
			defer wg.Done()
			k := first.NextStepKey(edgeID)
			mu.Lock()
			got[edgeID] = k
			mu.Unlock()
		}(e)
	}
	wg.Wait()

	// Redelivery: same parent identity, sequential order this time.
	replay := NewRunInfo("run-1", "run-1.root")
	for _, e := range edges {
		if want := replay.NextStepKey(e); got[e] != want {
			t.Fatalf("edge %s: concurrent key %q != replay key %q", e, got[e], want)
		}
	}
}

func TestNextStepKey_RepeatedEmitsSameEdge(t *testing.T) {
	r1 := NewRunInfo("run-2", "seed")
	a1, a2 := r1.NextStepKey("edge"), r1.NextStepKey("edge")
	if a1 == a2 {
		t.Fatal("two emits over the same edge must get distinct keys")
	}

	r2 := NewRunInfo("run-2", "seed")
	if b1, b2 := r2.NextStepKey("edge"), r2.NextStepKey("edge"); b1 != a1 || b2 != a2 {
		t.Fatalf("replay must reproduce the sequence: got (%q,%q) want (%q,%q)", b1, b2, a1, a2)
	}
}

// Keys must not grow with chain depth — each hop's key is runID + fixed hash.
func TestNextStepKey_FixedLength(t *testing.T) {
	r := NewRunInfo("run-3", "seed")
	k := r.NextStepKey("edge-a")
	wantLen := len("run-3") + 1 + 16
	if len(k) != wantLen {
		t.Fatalf("key length %d, want %d (%q)", len(k), wantLen, k)
	}
	// simulate a deep chain: child of child of child…
	for i := 0; i < 50; i++ {
		r = NewRunInfo("run-3", k)
		k = r.NextStepKey(fmt.Sprintf("edge-%d", i))
	}
	if len(k) != wantLen {
		t.Fatalf("depth-50 key length %d, want %d", len(k), wantLen)
	}
}

func TestMintRun_UniqueAndSeeded(t *testing.T) {
	a, b := MintRun(), MintRun()
	if a.RunID == "" || a.StepKey == "" {
		t.Fatal("minted run must have id and seed step key")
	}
	if a.RunID == b.RunID {
		t.Fatal("minted runs must be unique")
	}
}

func TestRunContext_RoundTrip(t *testing.T) {
	if _, ok := RunFrom(context.Background()); ok {
		t.Fatal("bare context must carry no run")
	}
	r := MintRun()
	ctx := WithRun(context.Background(), r)
	got, ok := RunFrom(ctx)
	if !ok || got != r {
		t.Fatalf("round trip failed: %v %v", got, ok)
	}
}
