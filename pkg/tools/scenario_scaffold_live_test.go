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
