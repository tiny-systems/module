package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

type creds struct {
	APIKey string `json:"apiKey" format:"password"`
	Model  string `json:"model"`
}

type request struct {
	Context map[string]any `json:"context"`
	Creds   creds          `json:"creds"`
	Prompt  string         `json:"prompt"`
}

// The component says which field holds a credential. Everything else in this
// package is a guess standing in for that declaration.
func TestDeclaredFieldIsMasked(t *testing.T) {
	got, changed := Declared(creds{APIKey: "sk-ant-whatever", Model: "claude-sonnet-5"})
	if !changed {
		t.Fatal("a declared secret was left alone")
	}
	out := got.(creds)
	if out.APIKey == "sk-ant-whatever" {
		t.Fatal("the key survived")
	}
	if out.Model != "claude-sonnet-5" {
		t.Errorf("model = %q — everything else must survive intact", out.Model)
	}
}

// The point of using the declaration: it catches a secret whose name and value
// give nothing away. Neither heuristic in this package would find this one.
func TestADeclaredSecretWithAnInnocentNameAndValue(t *testing.T) {
	type odd struct {
		X string `json:"x" format:"password"`
	}
	got, changed := Declared(odd{X: "hunter2"})
	if !changed || got.(odd).X == "hunter2" {
		t.Fatalf("a declared secret named x was missed: %+v", got)
	}
	if IsSecretKey("x") {
		t.Error("this test is pointless if the name heuristic already catches it")
	}
	if masked, _ := TextByShape("hunter2"); masked != "hunter2" {
		t.Error("this test is pointless if the shape heuristic already catches it")
	}
}

func TestNestedStructsAreReached(t *testing.T) {
	got, changed := Declared(request{
		Creds:  creds{APIKey: "sk-ant-x"},
		Prompt: "why is my pod crashlooping",
	})
	if !changed {
		t.Fatal("a nested declared secret was left alone")
	}
	out := got.(request)
	if out.Creds.APIKey == "sk-ant-x" {
		t.Fatal("nested key survived")
	}
	if out.Prompt != "why is my pod crashlooping" {
		t.Error("the payload around it was damaged")
	}
}

// A payload with nothing declared comes back untouched and unchanged, so the
// caller can skip copying — which is almost every message.
func TestNothingDeclaredMeansNoCopy(t *testing.T) {
	in := request{Prompt: "hello"}
	got, changed := Declared(in)
	if changed {
		t.Fatal("reported a change with nothing declared")
	}
	if got.(request).Prompt != "hello" {
		t.Error("value altered")
	}
}

// The caller is about to hand this payload to a component. Masking must not
// reach back into what they still hold.
func TestTheOriginalIsNotMutated(t *testing.T) {
	in := creds{APIKey: "sk-ant-original"}
	if _, changed := Declared(in); !changed {
		t.Fatal("expected a change")
	}
	if in.APIKey != "sk-ant-original" {
		t.Fatalf("the caller's value was mutated: %q", in.APIKey)
	}
}

func TestSecretsInsideSlicesAndMaps(t *testing.T) {
	got, changed := Declared(map[string]any{
		"items": []any{creds{APIKey: "sk-ant-inside"}},
	})
	if !changed {
		t.Fatal("a secret inside a slice inside a map was missed")
	}
	if strings.Contains(renderish(got), "sk-ant-inside") {
		t.Fatalf("key survived: %v", got)
	}
}

func renderish(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
