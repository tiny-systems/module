package tools

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

func redactConfigSchema(m map[string]interface{}) {
	if cfg, ok := m["configuration"]; ok && cfg != nil {
		m["configuration"] = RedactSecrets(cfg)
	}
	if sch, ok := m["schema"]; ok && sch != nil {
		m["schema"] = RedactSchemaSecrets(sch)
	}
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
