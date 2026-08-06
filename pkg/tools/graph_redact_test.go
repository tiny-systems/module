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
	if inner["apiKey"] != RedactedValue {
		t.Errorf("config apiKey = %v, want redacted", inner["apiKey"])
	}
	if inner["question"] != "hi" {
		t.Errorf("non-secret config value over-redacted: %v", inner["question"])
	}

	props := handle["schema"].(map[string]interface{})["$defs"].(map[string]interface{})["Context"].(map[string]interface{})["properties"].(map[string]interface{})["context"].(map[string]interface{})["properties"].(map[string]interface{})
	apiKeySchema := props["apiKey"].(map[string]interface{})
	if apiKeySchema["default"] != RedactedValue {
		t.Errorf("schema apiKey default = %v, want redacted", apiKeySchema["default"])
	}
	if apiKeySchema["type"] != "string" {
		t.Errorf("schema structure mangled: %v", apiKeySchema)
	}
	questionSchema := props["question"].(map[string]interface{})
	if questionSchema["default"] != "hi" {
		t.Errorf("non-secret schema default over-redacted: %v", questionSchema["default"])
	}

	edgeCfg := elements[1]["data"].(map[string]interface{})["configuration"].(map[string]interface{})
	if edgeCfg["token"] != RedactedValue {
		t.Errorf("edge token = %v, want redacted", edgeCfg["token"])
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
	if !strings.Contains(string(b), "redacted") {
		t.Fatalf("nothing was redacted: %s", b)
	}
}

// TestRedactAnyUndecodable: bytes we cannot inspect must not ship.
func TestRedactAnyUndecodable(t *testing.T) {
	if got := redactAny([]byte(`{broken`), RedactSecrets); got != nil {
		t.Fatalf("undecodable bytes must be dropped, got %v", got)
	}
}
