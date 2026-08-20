package tools

import (
	"reflect"
	"strings"
	"testing"
)

func TestExtractScenarioPorts(t *testing.T) {
	spans := []TraceSpanInfo{
		{ // output-port span with an object payload — captured
			Port: "flow.js-module-v0.js-eval-1:response",
			Events: []TraceEventInfo{
				{Name: "data", Data: map[string]string{"payload": `{"outputData":{"ok":true}}`}},
			},
		},
		{ // edge span — skipped even though it has a payload
			From: "a:out", To: "b:request",
			Events: []TraceEventInfo{
				{Name: "data", Data: map[string]string{"payload": `{"x":1}`}},
			},
		},
		{ // null payload (signal out) — no shape, skipped
			Port: "flow.common-module-v0.signal-1:out",
			Events: []TraceEventInfo{
				{Name: "data", Data: map[string]string{"payload": "null"}},
			},
		},
	}
	got := ExtractScenarioPorts(spans)
	want := map[string]map[string]interface{}{
		"flow.js-module-v0.js-eval-1:response": {"outputData": map[string]interface{}{"ok": true}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extract mismatch:\n got %v\nwant %v", got, want)
	}
}

func TestRedactSecrets(t *testing.T) {
	in := map[string]interface{}{
		"context": map[string]interface{}{
			"apiKey":   "sk-ant-real-key",
			"question": "which pods restart",
		},
		"headers": []interface{}{
			map[string]interface{}{"key": "Authorization", "authorization": "Bearer xyz"},
		},
		"access_key": "AKIA...",
		"count":      float64(3),
	}
	got := RedactSecrets(in).(map[string]interface{})

	ctx := got["context"].(map[string]interface{})
	if ctx["apiKey"] != RedactedValue {
		t.Fatalf("apiKey not redacted: %v", ctx["apiKey"])
	}
	if ctx["question"] != "which pods restart" {
		t.Fatalf("non-secret clobbered: %v", ctx["question"])
	}
	hdr := got["headers"].([]interface{})[0].(map[string]interface{})
	if hdr["authorization"] != RedactedValue {
		t.Fatalf("authorization not redacted: %v", hdr["authorization"])
	}
	if got["access_key"] != RedactedValue {
		t.Fatalf("access_key not redacted: %v", got["access_key"])
	}
	if got["count"] != float64(3) {
		t.Fatalf("non-string mutated: %v", got["count"])
	}
	// input untouched
	if in["context"].(map[string]interface{})["apiKey"] != "sk-ant-real-key" {
		t.Fatal("input mutated")
	}
}

// TestRedactSkipsEmpty: an empty credential slot must stay empty — marking it
// turns a blank form field into one pre-filled with a marker users submit.
func TestRedactSkipsEmpty(t *testing.T) {
	out := RedactSecrets(map[string]interface{}{"apiKey": "", "token": "abc"}).(map[string]interface{})
	if out["apiKey"] != "" {
		t.Errorf("empty apiKey = %q, want empty", out["apiKey"])
	}
	if out["token"] != RedactedValue {
		t.Errorf("non-empty token = %q, want redacted", out["token"])
	}
}

// The key that actually reached etcd. A key-value store returns its contents
// under "value" — a name that says nothing about what it holds — so matching
// by field name alone let a real Anthropic key be stored as sample data and
// published with a solution.
//
// Capture is the point of entry: scrubbing at publish keeps a credential out
// of a shipped solution but not out of the cluster.
func TestRedactSecrets_CatchesAKeyUnderAnInnocuousName(t *testing.T) {
	captured := map[string]interface{}{
		"context": map[string]interface{}{"id": "anthropic-main"},
		"value":   "sk-ant-api03-" + strings.Repeat("A1b2C3d4", 11),
	}

	out, ok := RedactSecrets(captured).(map[string]interface{})
	if !ok {
		t.Fatalf("shape changed: %T", RedactSecrets(captured))
	}
	if got, _ := out["value"].(string); strings.Contains(got, "sk-ant-") {
		t.Fatalf("the key was stored: %q", got)
	}
	// The sample's structure is what it exists for; only the value goes.
	if _, present := out["context"]; !present {
		t.Fatal("redaction removed unrelated fields")
	}
}

// The rule that was already there must keep working: a credential-named field
// is hidden whatever it holds.
func TestRedactSecrets_StillCatchesCredentialNames(t *testing.T) {
	out, _ := RedactSecrets(map[string]interface{}{"apiKey": "hunter2"}).(map[string]interface{})
	if got, _ := out["apiKey"].(string); got == "hunter2" {
		t.Fatal("a field named apiKey was stored in the clear")
	}
}

// Ordinary sample data must survive untouched — a sample exists to pin the
// shape of real traffic, and rewriting it would defeat the purpose.
func TestRedactSecrets_LeavesOrdinaryDataAlone(t *testing.T) {
	in := map[string]interface{}{
		"name":     "broken-checkout-657f5f7dd-6mz5c",
		"restarts": float64(0),
		"phase":    "ImagePullBackOff",
	}
	out, _ := RedactSecrets(in).(map[string]interface{})
	for k, want := range in {
		if out[k] != want {
			t.Errorf("%s = %v, want %v", k, out[k], want)
		}
	}
}
