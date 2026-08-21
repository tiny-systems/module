package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeReplay struct {
	project, traceID, at string
	outcome              ReplayOutcome
	err                  error
}

func (f *fakeReplay) ReplayRun(_ context.Context, project, traceID, at string) (ReplayOutcome, error) {
	f.project, f.traceID, f.at = project, traceID, at
	return f.outcome, f.err
}

func replayCall(t *testing.T, f *fakeReplay, input map[string]interface{}) (ToolResult, map[string]interface{}) {
	t.Helper()
	res := NewReplayRunTool().Execute(context.Background(), ExecutionContext{ProjectName: "proj", ReplayRunner: f}, input)
	out, _ := res.Output.(map[string]interface{})
	return res, out
}

func TestReplayPassesTheHopThrough(t *testing.T) {
	f := &fakeReplay{outcome: ReplayOutcome{Hop: "a:out → b:in", Unchanged: true, Compared: 4}}
	res, out := replayCall(t, f, map[string]interface{}{"trace_id": "abc", "at": "b:in"})

	if !res.Success {
		t.Fatalf("failed: %s", res.Error)
	}
	if f.traceID != "abc" || f.at != "b:in" || f.project != "proj" {
		t.Fatalf("called with %s / %s / %s", f.project, f.traceID, f.at)
	}
	if out["unchanged"] != true || out["compared"] != 4 {
		t.Fatalf("out = %+v", out)
	}
}

// "unchanged" with nothing compared is not good news, and must not read like
// it: a replay that reached nothing and one that matched everything would
// otherwise look identical.
func TestNothingComparedIsCalledOut(t *testing.T) {
	f := &fakeReplay{outcome: ReplayOutcome{Hop: "a:out → b:in", Unchanged: true, Compared: 0}}
	_, out := replayCall(t, f, map[string]interface{}{"trace_id": "abc"})

	hint, _ := out["hint"].(string)
	if !strings.Contains(hint, "proves nothing") {
		t.Fatalf("hint = %q", hint)
	}
}

func TestChangesAreReturnedWithGuidance(t *testing.T) {
	f := &fakeReplay{outcome: ReplayOutcome{
		Hop: "a:out → b:in", Compared: 3,
		Changes: []string{"b:in changed shape", "c:in no longer receives anything"},
	}}
	_, out := replayCall(t, f, map[string]interface{}{"trace_id": "abc"})

	changes, _ := out["changes"].([]string)
	if len(changes) != 2 {
		t.Fatalf("changes = %v", changes)
	}
	if hint, _ := out["hint"].(string); !strings.Contains(hint, "module release") {
		t.Errorf("hint does not help read the result: %q", hint)
	}
}

func TestTraceIDIsRequired(t *testing.T) {
	f := &fakeReplay{}
	if res, _ := replayCall(t, f, map[string]interface{}{}); res.Success {
		t.Fatal("accepted a call with no trace")
	}
}

// A refusal from the runner — a redacted mid-chain hop, say — has to reach the
// caller as the explanation it is.
func TestARunnerRefusalIsSurfaced(t *testing.T) {
	f := &fakeReplay{err: errors.New("this hop carries a redacted credential")}
	res, _ := replayCall(t, f, map[string]interface{}{"trace_id": "abc", "at": "llm:request"})
	if res.Success || !strings.Contains(res.Error, "redacted") {
		t.Fatalf("res = %+v", res)
	}
}
