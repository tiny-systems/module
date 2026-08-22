// Package evals is the contract for checking that a flow still does what it
// claims: the spec an author writes and the judgement applied to a run.
//
// It lives in the SDK rather than in one host because two hosts must reach the
// same verdict on the same run — an eval that passes locally and fails on the
// platform is worse than no eval. Firing the trigger and collecting the run
// belong to whoever has the cluster; deciding whether the claim held belongs
// here, where there is nothing to differ about.
//
// The project has more built than it has verified. Nothing tells you when
// something quietly stops being true: an overlay lied about node health for as
// long as it existed, a credential sat inert in a resource while the runtime
// logged the complaint every fifteen seconds, a command printed a sentence
// instead of doing its job. Every one of those was found by a person stumbling
// into it.
//
// An eval is the smallest thing that would have caught them: fire a real
// trigger on a real cluster, wait for the run, and assert on what actually
// arrived. It knows nothing about any module — it speaks nodes, ports,
// payloads and traces, so a flow made of components that did not exist when
// this was written is checked the same way.
package evals

import (
	"fmt"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// Spec is one eval: a trigger, and what should be true afterwards.
type Spec struct {
	// Name is what a failure reports. Write it as the claim being checked —
	// "pod-watch diagnoses a crashlooping pod" — so a red line in CI reads as
	// a statement that stopped being true.
	Name string `json:"name"`

	// Flow is informational: which flow this exercises. The trigger addresses
	// a node directly, so nothing depends on it.
	Flow string `json:"flow,omitempty"`

	Trigger Trigger `json:"trigger"`

	// Timeout bounds the wait for the run to finish. Default 60s.
	Timeout Duration `json:"timeout,omitempty"`

	Expect Expect `json:"expect,omitempty"`

	// Path is where this was loaded from, for reporting. Not part of the file.
	Path string `json:"-"`
}

// Trigger is the message that starts the run.
type Trigger struct {
	// Node addresses the node to fire at. A full resource name works; so does
	// any unambiguous suffix ("signal-f2b7b"), because the flow-id prefix
	// changes when a project is re-imported and an eval should survive that.
	Node string `json:"node"`

	// Port defaults to _control, the port a trigger's Send button writes.
	Port string `json:"port,omitempty"`

	// Data is the payload, verbatim.
	Data map[string]interface{} `json:"data,omitempty"`
}

// Expect is what must hold once the run has settled.
type Expect struct {
	// Errors is the number of UNHANDLED errors the run may produce — failures
	// that escaped, with nothing catching them. Nil means zero: the common
	// case, and the one worth defaulting to, since an eval that tolerates
	// errors by accident checks nothing.
	//
	// A failure routed out of an enabled error port does not count. That is a
	// flow handling a fault, which is the flow working; counting it would mean
	// a flow with any recovery path could never claim `errors: 0`, so the most
	// fault-tolerant flows would carry the weakest assertions. Assert what the
	// caught error then DID, with `arrives` on the error port.
	Errors *int `json:"errors,omitempty"`

	// Arrives asserts on payloads that reached a port.
	Arrives []Arrival `json:"arrives,omitempty"`

	// Usage bounds what the run consumed, keyed by the unit a component
	// reported. A loop that silently doubles its LLM calls still passes every
	// other check.
	Usage map[string]Bound `json:"usage,omitempty"`
}

// Arrival asserts on the payload that reached one port.
type Arrival struct {
	// At is "<node>:<port>", with the same suffix matching as Trigger.Node.
	At string `json:"at"`

	// Path is a JSONPath into the payload — the same dialect the expression
	// engine and kv use. Empty means the payload as a whole.
	Path string `json:"path,omitempty"`

	// Exactly one of these. Absent means "something arrived here at all",
	// which is a real assertion: most breakage is a message that never came.
	Equals   interface{} `json:"equals,omitempty"`
	Contains string      `json:"contains,omitempty"`
	Matches  string      `json:"matches,omitempty"`
	Exists   *bool       `json:"exists,omitempty"`
	Min      *float64    `json:"min,omitempty"`
	Max      *float64    `json:"max,omitempty"`
}

// Bound is a numeric range for a usage unit.
type Bound struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// Duration accepts "45s" in YAML rather than a nanosecond count.
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("bad duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON writes the duration back the way it was written — "45s", not a
// nanosecond count. An eval an agent saved has to be one a person can read and
// edit, or the file is a black box that only tooling can touch.
func (d Duration) MarshalJSON() ([]byte, error) {
	if d == 0 {
		return []byte(`""`), nil
	}
	return []byte(`"` + time.Duration(d).String() + `"`), nil
}

func (d Duration) Or(fallback time.Duration) time.Duration {
	if d == 0 {
		return fallback
	}
	return time.Duration(d)
}

// Marshal writes a spec as the YAML a repository holds. The shape is the same
// one Parse reads, so an eval written by an agent and one written by hand are
// the same file.
func Marshal(specs []Spec) ([]byte, error) {
	if len(specs) == 1 {
		return yaml.Marshal(specs[0])
	}
	return yaml.Marshal(specs)
}

// Parse reads one eval file. A file may hold several evals as a YAML list, so
// a flow's checks can live together.
func Parse(path string, data []byte) ([]Spec, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}

	var specs []Spec
	if strings.HasPrefix(trimmed, "-") {
		if err := yaml.Unmarshal(data, &specs); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	} else {
		var one Spec
		if err := yaml.Unmarshal(data, &one); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		specs = []Spec{one}
	}

	for i := range specs {
		specs[i].Path = path
		if err := specs[i].validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return specs, nil
}

func (s *Spec) validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("eval has no name — the name is what a failure reports")
	}
	if strings.TrimSpace(s.Trigger.Node) == "" {
		return fmt.Errorf("%s: trigger.node is required — an eval that fires nothing checks nothing", s.Name)
	}
	if s.Trigger.Port == "" {
		s.Trigger.Port = "_control"
	}
	for i, a := range s.Expect.Arrives {
		if !strings.Contains(a.At, ":") {
			return fmt.Errorf("%s: expect.arrives[%d].at must be \"<node>:<port>\", got %q", s.Name, i, a.At)
		}
	}
	return nil
}
