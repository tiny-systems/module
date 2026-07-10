package utils

import (
	"strings"
	"testing"
)

// jsEvalSchema mirrors the shape the reflector emits for js_eval Settings:
// a top-level $ref into $defs, outputData required + configurable, script
// required but NOT configurable (must not be flagged by this check).
const jsEvalSchema = `{
  "$ref": "#/$defs/Settings",
  "$defs": {
    "Settings": {
      "type": "object",
      "required": ["outputData", "script"],
      "properties": {
        "outputData": {"configurable": true, "title": "Output object",
          "description": "Downstream edges from this node are validated against this shape."},
        "inputData":  {"configurable": true, "title": "Input object"},
        "script":     {"title": "Script"}
      }
    }
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
			// script is required but NOT configurable — must never be flagged.
			if strings.Contains(joined, "Script") {
				t.Errorf("%s: non-configurable required field 'script' was wrongly flagged: %q", c.name, joined)
			}
		}
	}
}

func TestSettingsIssues_NoSchema(t *testing.T) {
	if got := SettingsIssues(nil, []byte(`{}`)); got != nil {
		t.Errorf("nil schema should yield no issues, got %v", got)
	}
}
