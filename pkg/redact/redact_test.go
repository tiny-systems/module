package redact

import "testing"

func TestSecrets(t *testing.T) {
	in := map[string]interface{}{
		"apiKey":    "sk-live-123",
		"expr":      "{{$.context.apiKey}}",
		"blank":     "",
		"question":  "is anything unhealthy?",
		"headers":   []interface{}{map[string]interface{}{"authorization": "Bearer abc"}},
		"nested":    map[string]interface{}{"context": map[string]interface{}{"token": "t-1"}},
		"apiKeyRef": "{{$.k}}",
	}
	out := Secrets(in).(map[string]interface{})

	if out["apiKey"] != Value {
		t.Errorf("literal secret not redacted: %v", out["apiKey"])
	}
	if out["expr"] != "{{$.context.apiKey}}" {
		t.Errorf("expression must survive: %v", out["expr"])
	}
	if out["apiKeyRef"] != "{{$.k}}" {
		t.Errorf("expression under secret-shaped key must survive: %v", out["apiKeyRef"])
	}
	if out["blank"] != "" {
		t.Errorf("empty slot must stay empty: %q", out["blank"])
	}
	if out["question"] != "is anything unhealthy?" {
		t.Error("non-secret value must survive")
	}
	hdr := out["headers"].([]interface{})[0].(map[string]interface{})
	if hdr["authorization"] != Value {
		t.Errorf("secret in array not redacted: %v", hdr["authorization"])
	}
	nested := out["nested"].(map[string]interface{})["context"].(map[string]interface{})
	if nested["token"] != Value {
		t.Errorf("nested secret not redacted: %v", nested["token"])
	}
	// input untouched
	if in["apiKey"] != "sk-live-123" {
		t.Error("input was mutated")
	}
}
