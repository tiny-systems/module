package utils

import (
	"strings"
	"testing"
)

// jsEvalSchema mirrors the REAL shape the reflector emits for js_eval
// Settings: a top-level $ref into $defs, and each property is itself a $ref
// to a generated def — the `configurable` flag + title/description live on
// the DEF (Outputdata), not the property. script $refs a NON-configurable
// def and must never be flagged. modules is required but non-configurable
// (empty [] is legitimate) — also must not be flagged.
const jsEvalSchema = `{
  "$ref": "#/$defs/Settings",
  "$defs": {
    "Settings": {
      "type": "object",
      "required": ["outputData", "script", "modules"],
      "properties": {
        "outputData": {"$ref": "#/$defs/Outputdata", "propertyOrder": 3},
        "inputData":  {"$ref": "#/$defs/Inputdata", "propertyOrder": 2},
        "script":     {"$ref": "#/$defs/Script", "propertyOrder": 4},
        "modules":    {"type": "array"}
      }
    },
    "Outputdata": {"type": "object", "configurable": true, "title": "Output object",
      "description": "Downstream edges from this node are validated against this shape."},
    "Inputdata":  {"type": "object", "configurable": true, "title": "Input object"},
    "Script":     {"type": "object", "title": "Script", "required": ["content"]}
  }
}`

func TestSettingsIssues_RequiredConfigurableUnset(t *testing.T) {
	cases := []struct {
		name       string
		config     string
		wantIssue  bool
		wantSubstr string
	}{
		{"outputData absent → issue", `{"script":{"content":"x"}}`, true, "Output object"},
		{"outputData null → issue", `{"outputData":null}`, true, "Output object"},
		{"outputData empty object → issue", `{"outputData":{}}`, true, "Output object"},
		{"outputData filled → clean", `{"outputData":{"embedding":[0.1]}}`, false, ""},
		{"empty config → issue", `{}`, true, "Output object"},
	}
	for _, c := range cases {
		issues := SettingsIssues([]byte(jsEvalSchema), []byte(c.config))
		got := len(issues) > 0
		if got != c.wantIssue {
			t.Errorf("%s: wantIssue=%v got=%v (%v)", c.name, c.wantIssue, got, issues)
			continue
		}
		if c.wantIssue {
			joined := strings.Join(issues, " | ")
			if !strings.Contains(joined, c.wantSubstr) {
				t.Errorf("%s: expected %q in %q", c.name, c.wantSubstr, joined)
			}
			// script + modules are required but NOT configurable — a required
			// scalar/array that rides a default must never be flagged.
			if strings.Contains(joined, "Script") || strings.Contains(strings.ToLower(joined), "modules") {
				t.Errorf("%s: a non-configurable required field was wrongly flagged: %q", c.name, joined)
			}
		}
	}
}

func TestSettingsIssues_NoSchema(t *testing.T) {
	if got := SettingsIssues(nil, []byte(`{}`)); got != nil {
		t.Errorf("nil schema should yield no issues, got %v", got)
	}
}
