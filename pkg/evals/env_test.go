package evals

import (
	"strings"
	"testing"
)

func TestExpandEnvFillsPlaceholdersFromTheEnvironment(t *testing.T) {
	t.Setenv("EVAL_TEST_KEY", "sk-not-real-abc")

	s := &Spec{Trigger: Trigger{Data: map[string]interface{}{
		"send": true,
		"context": map[string]interface{}{
			"apiKey": "${EVAL_TEST_KEY}",
			"pod":    "web-1",
		},
	}}}

	if err := s.ExpandEnv(); err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	ctx := s.Trigger.Data["context"].(map[string]interface{})
	if ctx["apiKey"] != "sk-not-real-abc" {
		t.Errorf("apiKey = %v, want the environment value", ctx["apiKey"])
	}
	if ctx["pod"] != "web-1" {
		t.Errorf("pod = %v, want it untouched", ctx["pod"])
	}
	if s.Trigger.Data["send"] != true {
		t.Error("non-string value was altered")
	}
}

// A missing variable must stop the run. Firing with an empty credential
// produces an authentication failure three hops downstream, and whoever reads
// that failure goes looking for a broken flow instead of an unset variable.
func TestExpandEnvRefusesToFireWithAnUnsetVariable(t *testing.T) {
	s := &Spec{
		Name:    "the agent answers",
		Trigger: Trigger{Data: map[string]interface{}{"context": map[string]interface{}{"apiKey": "${EVAL_ABSENT_VAR}"}}},
	}

	err := s.ExpandEnv()
	if err == nil {
		t.Fatal("an unset variable was allowed through")
	}
	if !strings.Contains(err.Error(), "EVAL_ABSENT_VAR") {
		t.Errorf("error does not name the variable: %v", err)
	}
}

// Every missing variable at once, so a person fixes their environment in one
// pass instead of rerunning to discover the next one.
func TestExpandEnvReportsEveryMissingVariable(t *testing.T) {
	s := &Spec{Trigger: Trigger{Data: map[string]interface{}{
		"a": "${EVAL_MISSING_ONE}",
		"b": []interface{}{"${EVAL_MISSING_TWO}"},
	}}}

	err := s.ExpandEnv()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"EVAL_MISSING_ONE", "EVAL_MISSING_TWO"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error omits %s: %v", want, err)
		}
	}
}

// The error must not carry the value of anything it DID resolve — an eval
// failure gets pasted into issues and CI logs.
func TestExpandEnvErrorNeverCarriesAResolvedValue(t *testing.T) {
	t.Setenv("EVAL_PRESENT", "sk-super-secret-value")

	s := &Spec{Trigger: Trigger{Data: map[string]interface{}{
		"good": "${EVAL_PRESENT}",
		"bad":  "${EVAL_STILL_ABSENT}",
	}}}

	err := s.ExpandEnv()
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-super-secret-value") {
		t.Error("the error message leaked a resolved credential")
	}
}

func TestExpandEnvWalksNestedContainers(t *testing.T) {
	t.Setenv("EVAL_DEEP", "found")

	s := &Spec{Trigger: Trigger{Data: map[string]interface{}{
		"list": []interface{}{
			map[string]interface{}{"headers": []interface{}{"${EVAL_DEEP}"}},
		},
	}}}
	if err := s.ExpandEnv(); err != nil {
		t.Fatal(err)
	}
	got := s.Trigger.Data["list"].([]interface{})[0].(map[string]interface{})["headers"].([]interface{})[0]
	if got != "found" {
		t.Errorf("nested value = %v, want %q", got, "found")
	}
}

// A spec with no placeholders is the common case and must not need an
// environment at all.
func TestExpandEnvIsANoOpWithoutPlaceholders(t *testing.T) {
	s := &Spec{Trigger: Trigger{Data: map[string]interface{}{"send": true, "note": "$ is fine, so is {braces}"}}}
	if err := s.ExpandEnv(); err != nil {
		t.Fatalf("ExpandEnv on a plain spec: %v", err)
	}
	if s.Trigger.Data["note"] != "$ is fine, so is {braces}" {
		t.Errorf("note = %v, want it untouched", s.Trigger.Data["note"])
	}
}

// A variable set to empty is set. It is a deliberate choice ("no credential"),
// distinct from never having been exported, and must not be reported missing.
func TestExpandEnvAcceptsAnExplicitlyEmptyVariable(t *testing.T) {
	t.Setenv("EVAL_DELIBERATELY_EMPTY", "")
	s := &Spec{Trigger: Trigger{Data: map[string]interface{}{"k": "${EVAL_DELIBERATELY_EMPTY}"}}}
	if err := s.ExpandEnv(); err != nil {
		t.Fatalf("an exported-but-empty variable was treated as missing: %v", err)
	}
	if s.Trigger.Data["k"] != "" {
		t.Errorf("k = %v, want empty", s.Trigger.Data["k"])
	}
}

// Partial substitution, since a value is often a prefix plus a secret.
func TestExpandEnvSubstitutesWithinAString(t *testing.T) {
	t.Setenv("EVAL_HOST", "example.com")
	s := &Spec{Trigger: Trigger{Data: map[string]interface{}{"url": "https://${EVAL_HOST}/v1"}}}
	if err := s.ExpandEnv(); err != nil {
		t.Fatal(err)
	}
	if s.Trigger.Data["url"] != "https://example.com/v1" {
		t.Errorf("url = %v", s.Trigger.Data["url"])
	}
}
