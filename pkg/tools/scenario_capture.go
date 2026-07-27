package tools

import (
	"encoding/json"
	"regexp"
)

// Scenario capture — shared helpers for pinning a trace as a scenario
// (platform/docs/scenarios.md "Trace pinning flow"). A trace's output-port
// spans carry the real payload that flowed through each port; extracting them
// per port gives exactly the sample data the edge validator chain-walks
// against. Secrets are redacted before anything is persisted: pinned sample
// data lands in a TinyScenario CRD (etcd) and in project exports, and the doc
// is explicit that scenarios hold sanitized examples, not production values —
// an agent flow's context routinely carries an apiKey on every hop, so an
// unredacted pin would copy the key out of the message stream into config.

// secretKeyRe matches map keys that conventionally hold credentials. Matching
// is by key name, not value shape — the payload carries no schema at this
// point, so the field name is the only signal available.
var secretKeyRe = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|passw|authorization|bearer|credential|private[_-]?key|access[_-]?key)`)

// RedactedValue replaces every string that sat under a secret-looking key.
const RedactedValue = "<redacted>"

// ExtractScenarioPorts turns a trace's spans into per-port sample data:
// output-port spans only (edge spans describe transport, and their payloads
// duplicate what the source port already emitted), last data event wins, and
// only JSON-object payloads count — a bare "null" (e.g. a signal's out port)
// has no shape worth pinning.
func ExtractScenarioPorts(spans []TraceSpanInfo) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	for _, sp := range spans {
		if sp.Port == "" {
			continue // edge span (From/To) or malformed
		}
		for _, ev := range sp.Events {
			payload, ok := ev.Data["payload"]
			if !ok || payload == "" {
				continue
			}
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(payload), &obj); err != nil || obj == nil {
				continue
			}
			out[sp.Port] = obj
		}
	}
	return out
}

// RedactSecrets walks a decoded JSON value and replaces string values whose
// key looks credential-shaped with RedactedValue. Containers are recursed
// regardless of their own key, so context.apiKey and headers[0].authorization
// are both caught. The walk copies — the input is never mutated.
func RedactSecrets(v interface{}) interface{} {
	return redactValue("", v)
}

func redactValue(key string, v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[k] = redactValue(k, val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = redactValue(key, val)
		}
		return out
	case string:
		if key != "" && secretKeyRe.MatchString(key) {
			return RedactedValue
		}
		return x
	default:
		return v
	}
}
