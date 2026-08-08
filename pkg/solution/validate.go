// Package solution holds the rules a published solution must satisfy.
//
// It lives in the SDK because both publishing paths must apply them: the
// CLI before upload, and the platform's import on the way in. A rule that
// exists in only one of those is a rule with a bypass — the redaction
// rules had to be fixed twice for exactly that reason.
package solution

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/redact"
)

// checkWidgetShape enforces the two widget rules that a published solution
// cannot be sloppy about, because the installer meets them before anything
// else. Both are cheap to state and were each learned by shipping the
// opposite in a public solution.
func CheckWidgetShape(nodesMap map[string]v1alpha1.TinyNode) error {
	var problems []string

	for name, node := range nodesMap {
		if node.Labels[v1alpha1.DashboardLabel] == "" {
			continue // not a widget; these rules are about the user-facing surface
		}
		short := shortNodeName(name)

		// 1. A credential must not share a form with per-run inputs. Merging
		// them makes the user paste a key on every single run, and puts a
		// secret in the payload of every message. The fix is a dedicated
		// config widget feeding inject's persisted `config` port.
		if fields := settingsContextFields(node); len(fields) > 1 {
			for _, f := range fields {
				if redact.IsSecretKey(f) {
					problems = append(problems, fmt.Sprintf(
						"%s: widget form carries the credential %q alongside per-run inputs %v — send the credential once into inject.config from its own widget, and keep this form to what changes per run",
						short, f, without(fields, f)))
					break
				}
			}
		}

		// 2. A widget must be given the answer, not the whole message. A
		// `{{$}}` passthrough into a dashboard node renders the entire
		// pipeline state — history, credentials, scaffolding — as a wall of
		// form fields that grows with every run.
		for _, pc := range node.Spec.Ports {
			if pc.From == "" || len(pc.Configuration) == 0 {
				continue
			}
			var cfg map[string]interface{}
			if json.Unmarshal(pc.Configuration, &cfg) != nil {
				continue
			}
			for field, v := range cfg {
				if str, ok := v.(string); ok && strings.TrimSpace(str) == "{{$}}" {
					problems = append(problems, fmt.Sprintf(
						"%s: widget input %q maps the whole message ({{$}}) — map the specific value a person reads, e.g. {\"answer\": \"{{$.outputData.messages[0].content}}\"}",
						short, field))
				}
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf(`%d dashboard widget(s) would confuse whoever installs this:
  %s`, len(problems), strings.Join(problems, "\n  "))
}

// settingsContextFields returns the field names a widget's form presents.
func settingsContextFields(node v1alpha1.TinyNode) []string {
	for _, pc := range node.Spec.Ports {
		if pc.Port != v1alpha1.SettingsPort || len(pc.Configuration) == 0 {
			continue
		}
		var settings struct {
			Context map[string]interface{} `json:"context"`
		}
		if json.Unmarshal(pc.Configuration, &settings) != nil {
			return nil
		}
		fields := make([]string, 0, len(settings.Context))
		for k := range settings.Context {
			fields = append(fields, k)
		}
		sort.Strings(fields)
		return fields
	}
	return nil
}

func without(fields []string, drop string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != drop {
			out = append(out, f)
		}
	}
	return out
}

// shortNodeName trims the flow-hash and module prefix for readable errors.
func shortNodeName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}
