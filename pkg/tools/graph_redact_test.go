package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRedactGraphElements pins the exact leak shape found in production:
// a debug node whose _settings handle carried the full last message
// (context.context.apiKey) in BOTH configuration and, as defaults, in the
// runtime-generated schema.
func TestRedactGraphElements(t *testing.T) {
	raw := `[{
	 "id": "n1", "data": {"component": "debug", "handles": [
	  {"id": "_settings",
	   "configuration": {"context": {"context": {"apiKey": "sk-live-123", "question": "hi"}}},
	   "schema": {"$defs": {"Context": {"properties": {"context": {"properties": {"apiKey": {"type": "string", "default": "sk-live-123"}, "question": {"type": "string", "default": "hi"}}}}}}}
	  }]}},
	 {"id": "e1", "source": "n1", "data": {"configuration": {"token": "abc", "value": "{{$.x}}"}}}
	]`
	var elements []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &elements); err != nil {
		t.Fatal(err)
	}

	RedactGraphElements(elements)

	b, _ := json.Marshal(elements)
	s := string(b)
	if strings.Contains(s, "sk-live-123") {
		t.Fatalf("secret survived redaction: %s", s)
	}
	if strings.Contains(s, `"abc"`) {
		t.Fatalf("edge token survived: %s", s)
	}

	handle := elements[0]["data"].(map[string]interface{})["handles"].([]interface{})[0].(map[string]interface{})
	cfg := handle["configuration"].(map[string]interface{})
	inner := cfg["context"].(map[string]interface{})["context"].(map[string]interface{})
	if inner["apiKey"] != PublishedSecretValue {
		t.Errorf("config apiKey = %q, want blank", inner["apiKey"])
	}
	if inner["question"] != "hi" {
		t.Errorf("non-secret config value over-redacted: %v", inner["question"])
	}

	props := handle["schema"].(map[string]interface{})["$defs"].(map[string]interface{})["Context"].(map[string]interface{})["properties"].(map[string]interface{})["context"].(map[string]interface{})["properties"].(map[string]interface{})
	apiKeySchema := props["apiKey"].(map[string]interface{})
	if apiKeySchema["default"] != PublishedSecretValue {
		t.Errorf("schema apiKey default = %q, want blank — a pre-filled marker gets submitted as a credential", apiKeySchema["default"])
	}
	if apiKeySchema["type"] != "string" {
		t.Errorf("schema structure mangled: %v", apiKeySchema)
	}
	questionSchema := props["question"].(map[string]interface{})
	if questionSchema["default"] != "hi" {
		t.Errorf("non-secret schema default over-redacted: %v", questionSchema["default"])
	}

	edgeCfg := elements[1]["data"].(map[string]interface{})["configuration"].(map[string]interface{})
	if edgeCfg["token"] != PublishedSecretValue {
		t.Errorf("edge token = %q, want blank", edgeCfg["token"])
	}
	if edgeCfg["value"] != "{{$.x}}" {
		t.Errorf("expression value must survive: %v", edgeCfg["value"])
	}
}

// TestRedactGraphElementsRawMessage pins the publish-path shape:
// NodesToGraph keeps configuration/schema as json.RawMessage, which the
// first version of the redactor silently passed through unredacted.
func TestRedactGraphElementsRawMessage(t *testing.T) {
	elements := []map[string]interface{}{{
		"id": "n1",
		"data": map[string]interface{}{
			"component": "debug",
			"handles": []interface{}{map[string]interface{}{
				"id":            "_settings",
				"configuration": json.RawMessage(`{"context":{"apiKey":"sk-live-999"}}`),
				"schema":        json.RawMessage(`{"properties":{"apiKey":{"type":"string","default":"sk-live-999"}}}`),
			}},
		},
	}}

	RedactGraphElements(elements)

	b, _ := json.Marshal(elements)
	if strings.Contains(string(b), "sk-live-999") {
		t.Fatalf("RawMessage secret survived: %s", b)
	}
	// json.Marshal escapes "<" so match the bare word, not RedactedValue
	if strings.Contains(string(b), "sk-live") {
		t.Fatalf("secret survived: %s", b)
	}
}

// TestRedactAnyUndecodable: bytes we cannot inspect must not ship.
func TestRedactAnyUndecodable(t *testing.T) {
	if got := redactAny([]byte(`{broken`), RedactSecrets); got != nil {
		t.Fatalf("undecodable bytes must be dropped, got %v", got)
	}
}

func TestRedactBytes(t *testing.T) {
	got := RedactConfigurationBytes([]byte(`{"apiKey":"sk-1","q":"hi"}`))
	if strings.Contains(string(got), "sk-1") || !strings.Contains(string(got), `"q":"hi"`) {
		t.Fatalf("config bytes: %s", got)
	}
	got = RedactSchemaBytes([]byte(`{"properties":{"token":{"default":"sk-2"}}}`))
	if strings.Contains(string(got), "sk-2") {
		t.Fatalf("schema bytes: %s", got)
	}
	if RedactConfigurationBytes(nil) != nil || RedactConfigurationBytes([]byte("{oops")) != nil {
		t.Fatal("nil/undecodable must return nil")
	}
}

// TestRedactPreservesExpressions pins the failure that shipped a broken
// solution: an edge feeding a runtime key as {{$.context.apiKey}} had the
// expression rewritten to a literal marker, so the component received the
// marker as its credential and every call failed with invalid x-api-key.
func TestRedactPreservesExpressions(t *testing.T) {
	elements := []map[string]interface{}{{
		"id":     "e1",
		"source": "n1",
		"data": map[string]interface{}{
			"configuration": json.RawMessage(`{"apiKey":"{{$.context.apiKey}}","token":"sk-literal-secret","context":"{{$.context}}"}`),
		},
	}}

	RedactGraphElements(elements)

	cfg := elements[0]["data"].(map[string]interface{})["configuration"]
	var decoded map[string]interface{}
	if err := json.Unmarshal(cfg.(json.RawMessage), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["apiKey"] != "{{$.context.apiKey}}" {
		t.Errorf("expression rewritten to %q — the edge no longer reads the runtime key", decoded["apiKey"])
	}
	if decoded["token"] == "sk-literal-secret" {
		t.Error("literal secret survived redaction")
	}
}
