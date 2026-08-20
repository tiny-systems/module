package evals

import (
	"strings"
	"testing"
	"time"
)

func float(f float64) *float64 { return &f }
func boolp(b bool) *bool       { return &b }
func intp(i int) *int          { return &i }

// ---------- the spec file ----------

func TestParseReadsOneEval(t *testing.T) {
	specs, err := Parse("x.yaml", []byte(`
name: pod-watch answers
trigger:
  node: signal-abc
  data: {send: true}
timeout: 45s
expect:
  arrives:
    - at: debug-1:in
      path: $.count
      equals: 2
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("%d specs", len(specs))
	}
	s := specs[0]
	if s.Trigger.Port != "_control" {
		t.Errorf("port = %q, want the _control default", s.Trigger.Port)
	}
	if s.Timeout.Or(time.Minute) != 45*time.Second {
		t.Errorf("timeout = %v", s.Timeout.Or(time.Minute))
	}
	if s.Path != "x.yaml" {
		t.Errorf("path = %q, want the file it came from", s.Path)
	}
}

func TestParseReadsAListOfEvals(t *testing.T) {
	specs, err := Parse("x.yaml", []byte(`
- name: first
  trigger: {node: a}
- name: second
  trigger: {node: b}
`))
	if err != nil || len(specs) != 2 {
		t.Fatalf("specs = %v, err = %v", len(specs), err)
	}
}

// An eval with no name reports nothing useful when it fails, and one with no
// trigger checks nothing at all.
func TestParseRefusesAnEvalThatCannotReport(t *testing.T) {
	for name, doc := range map[string]string{
		"no name":    "trigger: {node: a}",
		"no trigger": "name: x",
		"bad at":     "name: x\ntrigger: {node: a}\nexpect: {arrives: [{at: debug}]}",
	} {
		if _, err := Parse("x.yaml", []byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// ---------- assertions ----------

func TestArrivalEquals(t *testing.T) {
	spec := Spec{Expect: Expect{Arrives: []Arrival{{At: "debug-1:in", Path: "$.count", Equals: 2}}}}
	got := Observed{Payloads: map[string][]string{"flow.mod.debug-1:in": {`{"count":2}`}}}

	if f := Check(spec, got); len(f) != 0 {
		t.Fatalf("failures = %v", f)
	}
}

// YAML gives an int, JSON gives a float. An eval author must not have to know.
func TestNumbersCompareAcrossTypes(t *testing.T) {
	spec := Spec{Expect: Expect{Arrives: []Arrival{{At: "d:in", Path: "$.n", Equals: 7}}}}
	got := Observed{Payloads: map[string][]string{"d:in": {`{"n":7.0}`}}}
	if f := Check(spec, got); len(f) != 0 {
		t.Fatalf("failures = %v", f)
	}
}

// LLM text varies. An eval that demands an exact answer fails on a working
// flow, so contains and matches exist.
func TestContainsAndMatches(t *testing.T) {
	got := Observed{Payloads: map[string][]string{"d:in": {`{"text":"the pod is CrashLoopBackOff"}`}}}

	pass := Spec{Expect: Expect{Arrives: []Arrival{{At: "d:in", Path: "$.text", Contains: "CrashLoop"}}}}
	if f := Check(pass, got); len(f) != 0 {
		t.Fatalf("contains failed: %v", f)
	}

	re := Spec{Expect: Expect{Arrives: []Arrival{{At: "d:in", Path: "$.text", Matches: "(?i)crashloop"}}}}
	if f := Check(re, got); len(f) != 0 {
		t.Fatalf("matches failed: %v", f)
	}
}

// The most common breakage is a message that never came, so an arrival with no
// condition is a real assertion.
func TestBareArrivalAssertsSomethingCame(t *testing.T) {
	spec := Spec{Expect: Expect{Arrives: []Arrival{{At: "debug-1:in"}}}}

	if f := Check(spec, Observed{Payloads: map[string][]string{"debug-1:in": {`{}`}}}); len(f) != 0 {
		t.Fatalf("a message did arrive: %v", f)
	}
	f := Check(spec, Observed{Payloads: map[string][]string{"other:in": {`{}`}}})
	if len(f) != 1 || !strings.Contains(f[0].What, "expected a message") {
		t.Fatalf("failures = %v", f)
	}
	if !strings.Contains(f[0].Got, "other:in") {
		t.Errorf("the failure should say where messages did land: %s", f[0].Got)
	}
}

// A port that receives several times passes if any message satisfies the
// assertion — otherwise every eval on a looping flow would depend on ordering.
func TestAnyMatchingMessageSatisfies(t *testing.T) {
	spec := Spec{Expect: Expect{Arrives: []Arrival{{At: "d:in", Path: "$.status", Equals: "done"}}}}
	got := Observed{Payloads: map[string][]string{"d:in": {`{"status":"working"}`, `{"status":"done"}`}}}
	if f := Check(spec, got); len(f) != 0 {
		t.Fatalf("failures = %v", f)
	}
}

// The flow-id prefix changes when a project is re-imported; an eval must
// survive that.
func TestPortsMatchBySuffix(t *testing.T) {
	spec := Spec{Expect: Expect{Arrives: []Arrival{{At: "debug-21578:in"}}}}
	got := Observed{Payloads: map[string][]string{"1d1a7c4a.tinysystems-common-module-v0.debug-21578:in": {`{}`}}}
	if f := Check(spec, got); len(f) != 0 {
		t.Fatalf("failures = %v", f)
	}
}

// Errors default to zero: an eval that tolerates them by accident checks
// nothing.
func TestErrorsAreZeroByDefault(t *testing.T) {
	spec := Spec{}
	f := Check(spec, Observed{Errors: 1})
	if len(f) != 1 || !strings.Contains(f[0].What, "0 errors") {
		t.Fatalf("failures = %v", f)
	}

	tolerant := Spec{Expect: Expect{Errors: intp(1)}}
	if f := Check(tolerant, Observed{Errors: 1}); len(f) != 0 {
		t.Fatalf("an expected error was reported: %v", f)
	}
}

// A loop that silently doubles its LLM calls passes every other check.
func TestUsageBounds(t *testing.T) {
	spec := Spec{Expect: Expect{Usage: map[string]Bound{"llm_calls": {Max: float(2)}}}}

	if f := Check(spec, Observed{Usage: map[string]float64{"llm_calls": 2}}); len(f) != 0 {
		t.Fatalf("within bound reported: %v", f)
	}
	f := Check(spec, Observed{Usage: map[string]float64{"llm_calls": 5}})
	if len(f) != 1 || !strings.Contains(f[0].Got, "5") {
		t.Fatalf("failures = %v", f)
	}
}

func TestUsageUnitNeverMeteredIsAFailure(t *testing.T) {
	spec := Spec{Expect: Expect{Usage: map[string]Bound{"llm_calls": {Min: float(1)}}}}
	f := Check(spec, Observed{})
	if len(f) != 1 || !strings.Contains(f[0].Got, "nothing metered") {
		t.Fatalf("failures = %v", f)
	}
}

func TestExistsAssertsPresenceAndAbsence(t *testing.T) {
	got := Observed{Payloads: map[string][]string{"d:in": {`{"error":null,"text":"hi"}`}}}

	present := Spec{Expect: Expect{Arrives: []Arrival{{At: "d:in", Path: "$.text", Exists: boolp(true)}}}}
	if f := Check(present, got); len(f) != 0 {
		t.Fatalf("failures = %v", f)
	}
	absent := Spec{Expect: Expect{Arrives: []Arrival{{At: "d:in", Path: "$.error", Exists: boolp(false)}}}}
	if f := Check(absent, got); len(f) != 0 {
		t.Fatalf("failures = %v", f)
	}
}

// A failure must carry the actual value, or the reader goes back to the
// cluster to find out what happened.
func TestFailureReportsWhatWasThere(t *testing.T) {
	spec := Spec{Expect: Expect{Arrives: []Arrival{{At: "d:in", Path: "$.count", Equals: 2}}}}
	f := Check(spec, Observed{Payloads: map[string][]string{"d:in": {`{"count":9}`}}})
	if len(f) != 1 {
		t.Fatalf("failures = %v", f)
	}
	if !strings.Contains(f[0].Got, "9") || !strings.Contains(f[0].What, "expected 2") {
		t.Fatalf("failure reads badly: %s", f[0])
	}
}
