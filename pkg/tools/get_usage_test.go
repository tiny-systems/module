package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeTraces struct {
	summaries []TraceSummary
	details   map[string][]TraceSpanInfo
	failOn    string
	lastLimit int
}

func (f *fakeTraces) ReadTraces(_ context.Context, _, _ string, _ time.Duration, _, limit int) ([]TraceSummary, error) {
	f.lastLimit = limit
	if limit < len(f.summaries) {
		return f.summaries[:limit], nil
	}
	return f.summaries, nil
}

func (f *fakeTraces) ReadTraceDetail(_ context.Context, _, traceID string) ([]TraceSpanInfo, error) {
	if traceID == f.failOn {
		return nil, context.DeadlineExceeded
	}
	return f.details[traceID], nil
}

func usageCtx(f *fakeTraces) ExecutionContext {
	return ExecutionContext{ProjectName: "proj", TraceReader: f}
}

func run(t *testing.T, f *fakeTraces, input map[string]interface{}) map[string]interface{} {
	t.Helper()
	res := NewGetUsageTool().Execute(context.Background(), usageCtx(f), input)
	if !res.Success {
		t.Fatalf("failed: %s", res.Error)
	}
	out, _ := res.Output.(map[string]interface{})
	return out
}

// The question is never what one run cost. It is what the window cost, and
// which node is responsible — both in one answer.
func TestUsageTotalsTheWindowAndNamesTheSpender(t *testing.T) {
	f := &fakeTraces{
		summaries: []TraceSummary{{ID: "t1"}, {ID: "t2"}},
		details: map[string][]TraceSpanInfo{
			"t1": {
				{To: "flow.mod.llm-1:request", Usage: map[string]float64{"llm_input_tokens": 100, "llm_calls": 1}},
				{To: "flow.mod.debug-1:in"},
			},
			"t2": {
				{To: "flow.mod.llm-1:request", Usage: map[string]float64{"llm_input_tokens": 300, "llm_calls": 1}},
				{To: "flow.mod.embed-9:request", Usage: map[string]float64{"llm_input_tokens": 20}},
			},
		},
	}

	out := run(t, f, nil)
	total := out["total"].(map[string]float64)
	if total["llm_input_tokens"] != 420 || total["llm_calls"] != 2 {
		t.Fatalf("total = %v", total)
	}
	if out["runs_with_usage"] != 2 {
		t.Errorf("runs_with_usage = %v", out["runs_with_usage"])
	}

	rows := out["by_node"].([]map[string]interface{})
	if len(rows) != 2 {
		t.Fatalf("%d nodes", len(rows))
	}
	if rows[0]["node"] != "flow.mod.llm-1" {
		t.Fatalf("biggest spender = %v, want the node that moved the most", rows[0]["node"])
	}
	if _, leaked := rows[0]["rank"]; leaked {
		t.Error("the ordering heuristic leaked into the response as if it meant something")
	}
}

// A total that quietly covers half the window is worse than no total.
func TestTruncationIsAnnounced(t *testing.T) {
	f := &fakeTraces{summaries: make([]TraceSummary, 5), details: map[string][]TraceSpanInfo{}}
	out := run(t, f, map[string]interface{}{"max_traces": float64(5)})
	if out["truncated"] == nil {
		t.Fatal("hit the ceiling and said nothing")
	}
}

// One unreadable trace must not lose the other 199.
func TestOneUnreadableTraceDoesNotLoseTheRest(t *testing.T) {
	f := &fakeTraces{
		summaries: []TraceSummary{{ID: "bad"}, {ID: "good"}},
		failOn:    "bad",
		details: map[string][]TraceSpanInfo{
			"good": {{To: "n:request", Usage: map[string]float64{"llm_calls": 1}}},
		},
	}
	out := run(t, f, nil)
	if out["total"].(map[string]float64)["llm_calls"] != 1 {
		t.Fatalf("total = %v", out["total"])
	}
}

// Nothing metered has to say why, or it reads as a broken tool.
func TestAnEmptyWindowExplainsItself(t *testing.T) {
	f := &fakeTraces{details: map[string][]TraceSpanInfo{}}
	out := run(t, f, nil)
	if out["hint"] == nil || !strings.Contains(out["hint"].(string), "meter") {
		t.Fatalf("out = %v", out)
	}
}

func TestLookbackAndFlowArePassedThrough(t *testing.T) {
	f := &fakeTraces{details: map[string][]TraceSpanInfo{}}
	out := run(t, f, map[string]interface{}{"lookback_hours": float64(72), "max_traces": float64(10)})
	if out["window_hours"] != 72.0 {
		t.Errorf("window = %v", out["window_hours"])
	}
	if f.lastLimit != 10 {
		t.Errorf("limit = %d", f.lastLimit)
	}
}
