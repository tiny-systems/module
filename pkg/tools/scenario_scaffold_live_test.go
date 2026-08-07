package tools

import "testing"

// TestTargetConstrainedValues pins the case that made a scaffolded sample
// fail its own validation: an enum-constrained target field.
func TestTargetConstrainedValues(t *testing.T) {
	config := map[string]interface{}{
		"kind":      "{{$.context.kind}}",
		"name":      "{{$.context.name}}",
		"namespace": "prefix-{{$.context.ns}}",
	}
	schema := []byte(`{
	 "$ref":"#/$defs/Request",
	 "$defs":{"Request":{"properties":{
	   "kind":{"type":"string","enum":["Deployment","StatefulSet","DaemonSet"]},
	   "name":{"type":"string"},
	   "namespace":{"type":"string"}}}}}`)

	got := targetConstrainedValues(config, schema)

	if got["context.kind"] != "Deployment" {
		t.Errorf("enum field: got %v, want Deployment", got["context.kind"])
	}
	if _, ok := got["context.name"]; ok {
		t.Error("unconstrained field must not be pinned")
	}
	if _, ok := got["context.ns"]; ok {
		t.Error("interpolated (non-whole-string) expression must not be pinned")
	}
}

func TestTargetConstrainedValuesConst(t *testing.T) {
	got := targetConstrainedValues(
		map[string]interface{}{"method": "{{$.context.method}}"},
		[]byte(`{"properties":{"method":{"const":"POST"}}}`))
	if got["context.method"] != "POST" {
		t.Errorf("const field: got %v, want POST", got["context.method"])
	}
}

// TestShapelessInlineProperty pins llm_tools' out_<tool> shape: `input` is an
// inline property of the root def with no type and no properties (its shape
// is whatever the tool declared), not a $defs entry of its own.
func TestShapelessInlineProperty(t *testing.T) {
	schema := []byte(`{"$ref":"#/$defs/Toolcall","$defs":{
	 "Context":{"path":"$.context","configurable":true},
	 "Toolcall":{"path":"$","type":"object","properties":{
	   "context":{"$ref":"#/$defs/Context"},
	   "input":{"title":"Input","description":"Structured arguments per the tool's inputSchema."},
	   "messages":{"type":"array","items":{"$ref":"#/$defs/Message"}},
	   "toolUseId":{"type":"string"}}}}}`)

	got := shapelessFieldsIn(schema)

	var hasInput, hasContext, hasToolUseID bool
	for _, f := range got {
		switch f {
		case "input":
			hasInput = true
		case "context":
			hasContext = true
		case "toolUseId":
			hasToolUseID = true
		}
	}
	if !hasInput {
		t.Errorf("inline shapeless property `input` not detected, got %v", got)
	}
	if !hasContext {
		t.Errorf("configurable def `context` lost, got %v", got)
	}
	if hasToolUseID {
		t.Errorf("typed scalar `toolUseId` must not be scaffolded, got %v", got)
	}
}

// TestTargetConstrainedScalarTypes: a "<leaf>" marker in an integer field is
// itself a validation failure, so the declared type picks the value.
func TestTargetConstrainedScalarTypes(t *testing.T) {
	got := targetConstrainedValues(
		map[string]interface{}{
			"lines":  "{{$.input.lines}}",
			"follow": "{{$.input.follow}}",
			"name":   "{{$.input.name}}",
		},
		[]byte(`{"properties":{
		  "lines":{"type":"integer"},
		  "follow":{"type":"boolean"},
		  "name":{"type":"string"}}}`))

	if got["input.lines"] != 0 {
		t.Errorf("integer field: got %#v, want 0", got["input.lines"])
	}
	if got["input.follow"] != false {
		t.Errorf("boolean field: got %#v, want false", got["input.follow"])
	}
	if _, pinned := got["input.name"]; pinned {
		t.Error("string field must keep its descriptive placeholder")
	}
}
