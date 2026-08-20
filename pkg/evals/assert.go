package evals

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tiny-systems/ajson"
)

// Failure is one assertion that did not hold. It carries what was expected and
// what was actually there, because a red line that only says "failed" sends
// the reader back to the cluster to find out what happened.
type Failure struct {
	What string
	Got  string
}

func (f Failure) String() string {
	if f.Got == "" {
		return f.What
	}
	return f.What + " — got " + f.Got
}

// Observed is what a run actually produced, in the shape the assertions read.
// The runner fills it from a trace; nothing here knows about spans.
type Observed struct {
	// Payloads holds every payload that arrived, keyed by "<node>:<port>" as
	// the trace reported it. A port may receive several times, so the values
	// are ordered as they arrived.
	Payloads map[string][]string
	Errors   int
	Usage    map[string]float64
}

// Check evaluates a spec against what happened. An empty result is a pass.
func Check(spec Spec, got Observed) []Failure {
	var failures []Failure

	wantErrors := 0
	if spec.Expect.Errors != nil {
		wantErrors = *spec.Expect.Errors
	}
	if got.Errors != wantErrors {
		failures = append(failures, Failure{
			What: fmt.Sprintf("expected %d errors", wantErrors),
			Got:  fmt.Sprintf("%d", got.Errors),
		})
	}

	for _, a := range spec.Expect.Arrives {
		failures = append(failures, checkArrival(a, got)...)
	}

	for unit, bound := range spec.Expect.Usage {
		amount, seen := got.Usage[unit]
		if !seen {
			failures = append(failures, Failure{
				What: fmt.Sprintf("expected the run to meter %q", unit),
				Got:  "nothing metered under that unit",
			})
			continue
		}
		if bound.Min != nil && amount < *bound.Min {
			failures = append(failures, Failure{
				What: fmt.Sprintf("expected %s >= %v", unit, *bound.Min),
				Got:  fmt.Sprintf("%v", amount),
			})
		}
		if bound.Max != nil && amount > *bound.Max {
			failures = append(failures, Failure{
				What: fmt.Sprintf("expected %s <= %v", unit, *bound.Max),
				Got:  fmt.Sprintf("%v", amount),
			})
		}
	}

	return failures
}

// checkArrival tests one port's payloads. Several messages may have arrived;
// the assertion passes if ANY of them satisfies it, because a port that
// receives three times and is right once is right — the alternative would make
// every eval on a looping flow depend on message ordering.
func checkArrival(a Arrival, got Observed) []Failure {
	payloads := matchPayloads(got.Payloads, a.At)
	if len(payloads) == 0 {
		return []Failure{{
			What: fmt.Sprintf("expected a message at %s", a.At),
			Got:  describePorts(got.Payloads),
		}}
	}

	var lastSeen string
	for _, payload := range payloads {
		value, err := valueAt(payload, a.Path)
		if err != nil {
			lastSeen = err.Error()
			continue
		}
		if ok, seen := satisfies(a, value); ok {
			return nil
		} else {
			lastSeen = seen
		}
	}

	return []Failure{{
		What: fmt.Sprintf("at %s%s: %s", a.At, pathSuffix(a.Path), describeExpectation(a)),
		Got:  lastSeen,
	}}
}

func pathSuffix(path string) string {
	if path == "" {
		return ""
	}
	return " " + path
}

// matchPayloads resolves a port reference against what arrived. An exact key
// wins; otherwise a suffix match, so an eval survives the flow-id prefix
// changing when a project is re-imported.
func matchPayloads(payloads map[string][]string, ref string) []string {
	if exact, ok := payloads[ref]; ok {
		return exact
	}
	var out []string
	for key, values := range payloads {
		if strings.HasSuffix(key, ref) || strings.HasSuffix(key, "."+ref) {
			out = append(out, values...)
		}
	}
	return out
}

func describePorts(payloads map[string][]string) string {
	if len(payloads) == 0 {
		return "nothing arrived anywhere — did the trigger fire?"
	}
	keys := make([]string, 0, len(payloads))
	for k := range payloads {
		keys = append(keys, k)
	}
	if len(keys) > 6 {
		keys = append(keys[:6], fmt.Sprintf("… and %d more", len(payloads)-6))
	}
	return "messages arrived at " + strings.Join(keys, ", ")
}

// valueAt reads the payload, optionally through a JSONPath — the same dialect
// the expression engine and kv use, so an eval is written the way an edge is.
func valueAt(payload, path string) (interface{}, error) {
	if strings.TrimSpace(path) == "" {
		var whole interface{}
		if err := json.Unmarshal([]byte(payload), &whole); err != nil {
			return payload, nil // not JSON: the raw text is the value
		}
		return whole, nil
	}

	node, err := ajson.Unmarshal([]byte(payload))
	if err != nil {
		return nil, fmt.Errorf("payload is not JSON: %s", truncate(payload, 120))
	}
	result, err := ajson.Eval(node, path)
	if err != nil {
		return nil, fmt.Errorf("path did not resolve in %s", truncate(payload, 120))
	}
	value, err := result.Unpack()
	if err != nil {
		return nil, err
	}
	return value, nil
}

// satisfies applies the assertion, returning what was seen when it does not
// hold.
func satisfies(a Arrival, value interface{}) (bool, string) {
	seen := render(value)

	switch {
	case a.Exists != nil:
		present := value != nil
		return present == *a.Exists, seen

	case a.Equals != nil:
		return equalish(value, a.Equals), seen

	case a.Contains != "":
		return strings.Contains(seen, a.Contains), seen

	case a.Matches != "":
		re, err := regexp.Compile(a.Matches)
		if err != nil {
			return false, fmt.Sprintf("invalid regexp %q: %v", a.Matches, err)
		}
		return re.MatchString(seen), seen

	case a.Min != nil || a.Max != nil:
		n, ok := numeric(value)
		if !ok {
			return false, seen + " (not a number)"
		}
		if a.Min != nil && n < *a.Min {
			return false, seen
		}
		if a.Max != nil && n > *a.Max {
			return false, seen
		}
		return true, seen
	}

	// No condition given: arriving at all is the assertion.
	return true, seen
}

func describeExpectation(a Arrival) string {
	switch {
	case a.Exists != nil && *a.Exists:
		return "expected a value"
	case a.Exists != nil:
		return "expected no value"
	case a.Equals != nil:
		return fmt.Sprintf("expected %v", a.Equals)
	case a.Contains != "":
		return fmt.Sprintf("expected to contain %q", a.Contains)
	case a.Matches != "":
		return fmt.Sprintf("expected to match %q", a.Matches)
	case a.Min != nil && a.Max != nil:
		return fmt.Sprintf("expected between %v and %v", *a.Min, *a.Max)
	case a.Min != nil:
		return fmt.Sprintf("expected >= %v", *a.Min)
	case a.Max != nil:
		return fmt.Sprintf("expected <= %v", *a.Max)
	}
	return "expected a message"
}

// equalish compares without punishing the caller for YAML's types: a spec
// saying `equals: 2` must match a JSON number that arrived as 2.0.
func equalish(got, want interface{}) bool {
	if gn, ok := numeric(got); ok {
		if wn, ok := numeric(want); ok {
			return gn == wn
		}
	}
	if gb, ok := got.(bool); ok {
		if wb, ok := want.(bool); ok {
			return gb == wb
		}
	}
	return render(got) == render(want)
}

func numeric(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func render(v interface{}) string {
	switch value := v.(type) {
	case nil:
		return "null"
	case string:
		return value
	case float64:
		if value == float64(int64(value)) {
			return fmt.Sprintf("%d", int64(value))
		}
		return fmt.Sprintf("%v", value)
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
