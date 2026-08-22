package evals

import (
	"strings"
	"testing"
)

// A caught failure is the flow working.
//
// Error ports are the whole fault-tolerance story here, so a flow with a
// recovery path routes failures out of one deliberately. Counting those toward
// the error budget would mean the most fault-tolerant flows could never assert
// `errors: 0` — the assertion would get weaker exactly where the engineering
// got stronger.
func TestCaughtErrorsDoNotCountAgainstTheBudget(t *testing.T) {
	spec := Spec{Name: "recovery"} // Expect.Errors nil => zero unhandled

	got := Observed{Errors: 0, Handled: 3}
	if f := Check(spec, got); len(f) != 0 {
		t.Errorf("three caught errors failed a zero-unhandled expectation: %+v", f)
	}
}

func TestAnEscapedErrorStillFails(t *testing.T) {
	spec := Spec{Name: "recovery"}

	f := Check(spec, Observed{Errors: 1})
	if len(f) == 0 {
		t.Fatal("an unhandled error passed")
	}
	if !strings.Contains(f[0].What, "unhandled") {
		t.Errorf("failure should say unhandled: %q", f[0].What)
	}
}

// When the count is wrong AND something was caught, say so: an escaped failure
// and a caught one are otherwise indistinguishable in the report.
func TestFailureNamesTheCaughtOnes(t *testing.T) {
	f := Check(Spec{Name: "x"}, Observed{Errors: 2, Handled: 5})
	if len(f) == 0 {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(f[0].Got, "5 caught") {
		t.Errorf("report omits the caught count: %q", f[0].Got)
	}
}

// Half of any branch is the road not taken.
//
// "the approved path fired" only constrains routing when paired with "the
// denied path did not" — without it, a component that fires BOTH branches
// passes the eval while being obviously broken.
func TestSilentPortsMustReceiveNothing(t *testing.T) {
	spec := Spec{Expect: Expect{Silent: []string{"debug-denied:in"}}}

	quiet := Observed{Payloads: map[string][]string{
		"flow1.mod.debug-approved:in": {`{"ok":true}`},
	}}
	if f := Check(spec, quiet); len(f) != 0 {
		t.Errorf("a genuinely silent port was reported: %+v", f)
	}

	noisy := Observed{Payloads: map[string][]string{
		"flow1.mod.debug-denied:in": {`{"ok":true}`},
	}}
	f := Check(spec, noisy)
	if len(f) == 0 {
		t.Fatal("a port that received was not reported")
	}
	if !strings.Contains(f[0].What, "debug-denied:in") {
		t.Errorf("failure does not name the port: %q", f[0].What)
	}
}

// Suffix matching, same as Arrives — an eval must survive a re-import.
func TestSilentMatchesBySuffix(t *testing.T) {
	spec := Spec{Expect: Expect{Silent: []string{"debug-x:in"}}}
	got := Observed{Payloads: map[string][]string{"aaaa1111.tinysystems-common-module-v0.debug-x:in": {`{}`}}}
	if f := Check(spec, got); len(f) == 0 {
		t.Error("a suffix-named silent port was not matched")
	}
}
