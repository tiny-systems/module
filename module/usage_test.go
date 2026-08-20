package module

import (
	"context"
	"sync"
	"testing"
)

func TestMeterAccumulatesPerUnit(t *testing.T) {
	ctx, usage := WithUsage(context.Background())

	Meter(ctx, "input_tokens", 1200)
	Meter(ctx, "output_tokens", 300)
	Meter(ctx, "input_tokens", 800) // a second call in the same hop

	total := usage.Total()
	if total["input_tokens"] != 2000 {
		t.Errorf("input_tokens = %v, want both calls summed", total["input_tokens"])
	}
	if total["output_tokens"] != 300 {
		t.Errorf("output_tokens = %v", total["output_tokens"])
	}
}

// Units are the component's own strings and are never interpreted, so a
// component counting credits or rows is served by the same path as one counting
// tokens.
func TestUnitsAreOpaque(t *testing.T) {
	ctx, usage := WithUsage(context.Background())
	Meter(ctx, "usd_micros", 4200)
	Meter(ctx, "rows_scanned", 17)

	total := usage.Total()
	if total["usd_micros"] != 4200 || total["rows_scanned"] != 17 {
		t.Fatalf("total = %v", total)
	}
}

// A component must not have to ask whether metering is available. In a unit
// test or a direct Handle call there is no sink, and the call is a no-op.
func TestMeterOutsideAMeteredContextIsSafe(t *testing.T) {
	Meter(context.Background(), "input_tokens", 100) // must not panic
}

// A provider that reported nothing must not look like a hop that was measured
// and cost nothing — those are different facts, and only one of them is known.
func TestZeroAndNegativeAmountsAreNotRecorded(t *testing.T) {
	ctx, usage := WithUsage(context.Background())
	Meter(ctx, "input_tokens", 0)
	Meter(ctx, "output_tokens", -5)
	Meter(ctx, "", 10)

	if total := usage.Total(); total != nil {
		t.Fatalf("total = %v, want nothing recorded", total)
	}
}

// Nothing metered is nil rather than an empty map, so a hop that cost nothing
// writes no attributes at all.
func TestNothingMeteredReportsNothing(t *testing.T) {
	_, usage := WithUsage(context.Background())
	if total := usage.Total(); total != nil {
		t.Fatalf("total = %v, want nil", total)
	}
}

// A long-running component meters from its own goroutines through the same
// context. Reading the total while they run must not race.
func TestMeteringFromSeveralGoroutines(t *testing.T) {
	ctx, usage := WithUsage(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Meter(ctx, "requests", 1)
		}()
	}
	wg.Wait()

	if got := usage.Total()["requests"]; got != 50 {
		t.Fatalf("requests = %v, want 50", got)
	}
}

// The total is a copy: a reader holding it must not see later work appear in a
// map it already took.
func TestTotalIsASnapshot(t *testing.T) {
	ctx, usage := WithUsage(context.Background())
	Meter(ctx, "requests", 1)

	taken := usage.Total()
	Meter(ctx, "requests", 1)

	if taken["requests"] != 1 {
		t.Fatalf("the snapshot changed underneath the reader: %v", taken)
	}
	if usage.Total()["requests"] != 2 {
		t.Fatal("the later call was not recorded")
	}
}

// A nested context (a deadline, a value) must still reach the same sink — the
// hop is the unit of metering, not one function's context.
func TestMeteringSurvivesADerivedContext(t *testing.T) {
	ctx, usage := WithUsage(context.Background())
	derived, cancel := context.WithCancel(ctx)
	defer cancel()

	Meter(derived, "input_tokens", 42)
	if usage.Total()["input_tokens"] != 42 {
		t.Fatal("a derived context did not reach the hop's sink")
	}
}
