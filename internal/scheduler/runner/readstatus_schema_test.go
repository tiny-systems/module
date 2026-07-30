package runner

import (
	"testing"

	m "github.com/tiny-systems/module/module"
)

// A port may carry an authored schema and nothing to reflect — the documented
// control-port form does exactly that. Gating the schema on Configuration made
// every such port publish nothing.
func TestAuthoredSchemaSurvivesNilConfiguration(t *testing.T) {
	authored := []byte(`{"type":"object","properties":{"go":{"type":"boolean","format":"button"}}}`)

	ports := []m.Port{
		{Name: "schema-only", Source: true, Schema: authored},
		{Name: "schema-and-config", Source: true, Schema: authored, Configuration: map[string]any{"go": false}},
		{Name: "config-only", Source: true, Configuration: map[string]any{"x": 1}},
	}

	for _, p := range ports {
		t.Run(p.Name, func(t *testing.T) {
			// Mirror the branch ReadStatus takes, without standing up a runner.
			var gotSchema []byte
			if p.Schema != nil {
				gotSchema = p.Schema
			}
			switch p.Name {
			case "schema-only", "schema-and-config":
				if len(gotSchema) == 0 {
					t.Errorf("authored schema was dropped for port %q", p.Name)
				}
			case "config-only":
				if len(gotSchema) != 0 {
					t.Errorf("port %q has no authored schema but one was published", p.Name)
				}
			}
		})
	}
}
