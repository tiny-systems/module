package tools

import "encoding/json"

// Graph-element redaction — the publish-path counterpart of the scenario
// redaction above. A solution export renders nodes with full port
// configuration AND schema; components whose settings absorb runtime data
// (debug mirrors the last message it saw) leak whatever flowed through the
// graph — an agent flow's context routinely carries an apiKey on every hop.
// Redact both value channels before an export leaves the machine:
//   - handle/port configurations: any string under a credential-shaped key
//   - schemas: value-bearing fields (default, const, examples) of any
//     property whose NAME is credential-shaped — runtime-generated schemas
//     embed live values as defaults so widgets can render them.

// RedactGraphElements redacts secrets in place-shaped copies inside the
// given export/graph elements (node handles and edge data). The elements
// slice is mutated; configuration/schema subtrees are replaced with
// redacted copies.
func RedactGraphElements(elements []map[string]interface{}) {
	for _, elem := range elements {
		data, _ := elem["data"].(map[string]interface{})
		if data == nil {
			continue
		}
		// edge elements carry configuration/schema directly on data
		redactConfigSchema(data)
		// node elements carry them per handle
		handles, _ := data["handles"].([]interface{})
		for _, h := range handles {
			if handle, ok := h.(map[string]interface{}); ok {
				redactConfigSchema(handle)
			}
		}
	}
}

// PublishedSecretValue is what a credential-shaped value becomes in a
// published export. Empty, not "<redacted>": these values land in the
// installer's widget as the field's DEFAULT, and a form pre-filled with a
// marker string is a field the user may well submit as-is — which reaches
// the provider as a bogus credential. Blank says "fill me in".
const PublishedSecretValue = ""

func redactConfigSchema(m map[string]interface{}) {
	if cfg, ok := m["configuration"]; ok && cfg != nil {
		m["configuration"] = redactAny(cfg, blankSecrets)
	}
	if sch, ok := m["schema"]; ok && sch != nil {
		m["schema"] = redactAny(sch, blankSchemaSecrets)
	}
}

// redactAny applies the given redactor to a value that is either already a
// decoded JSON tree (import path) or still raw bytes (NodesToGraph keeps
// configuration/schema as json.RawMessage). Raw bytes are decoded, redacted
// and re-encoded; undecodable bytes are DROPPED, not passed through — a
// value we cannot inspect must not ship.
func redactAny(v interface{}, redact func(interface{}) interface{}) interface{} {
	var raw []byte
	switch x := v.(type) {
	case json.RawMessage:
		raw = x
	case []byte:
		raw = x
	default:
		return redact(v)
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	out, err := json.Marshal(redact(decoded))
	if err != nil {
		return nil
	}
	return json.RawMessage(out)
}

// RedactSchemaSecrets walks a decoded JSON Schema and redacts the
// value-bearing fields (default, const, string examples) of any property
// whose name looks credential-shaped. Structure, types and titles are
// untouched — the schema stays valid for rendering; only embedded live
// values die.
func RedactSchemaSecrets(v interface{}) interface{} {
	return redactSchemaValue("", v)
}

func redactSchemaValue(key string, v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[k] = redactSchemaValue(k, val)
		}
		if key != "" && secretKeyRe.MatchString(key) {
			for _, f := range []string{"default", "const"} {
				if s, ok := out[f].(string); ok && s != "" {
					out[f] = RedactedValue
				}
			}
			if ex, ok := out["examples"].([]interface{}); ok {
				red := make([]interface{}, len(ex))
				for i, e := range ex {
					if _, isStr := e.(string); isStr {
						red[i] = RedactedValue
					} else {
						red[i] = e
					}
				}
				out["examples"] = red
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = redactSchemaValue(key, val)
		}
		return out
	default:
		return v
	}
}

// RedactConfigurationBytes and RedactSchemaBytes are the byte-level
// counterparts of RedactGraphElements for callers that hold port data as
// raw JSON (TinyNode CRs at solution import). nil in, nil out; undecodable
// bytes are dropped — a value we cannot inspect must not persist.
func RedactConfigurationBytes(b []byte) []byte {
	return redactBytes(b, RedactSecrets)
}

func RedactSchemaBytes(b []byte) []byte {
	return redactBytes(b, RedactSchemaSecrets)
}

func redactBytes(b []byte, redact func(interface{}) interface{}) []byte {
	if len(b) == 0 {
		return nil
	}
	out, _ := redactAny(json.RawMessage(b), redact).(json.RawMessage)
	return []byte(out)
}

// blankSecrets and blankSchemaSecrets are the publish-path redactors: same
// key-name heuristic, but the surviving value is empty rather than a marker.
func blankSecrets(v interface{}) interface{} {
	return replaceRedacted(RedactSecrets(v))
}

func blankSchemaSecrets(v interface{}) interface{} {
	return replaceRedacted(RedactSchemaSecrets(v))
}

func replaceRedacted(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			x[k] = replaceRedacted(val)
		}
		return x
	case []interface{}:
		for i, val := range x {
			x[i] = replaceRedacted(val)
		}
		return x
	case string:
		if x == RedactedValue {
			return PublishedSecretValue
		}
		return x
	default:
		return v
	}
}
