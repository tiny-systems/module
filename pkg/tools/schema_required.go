package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// unknownSettings reports node settings whose names don't exist on the
// component's _settings schema. A component silently ignores settings it
// doesn't declare, so a mis-named field (the classic: a script dropped in
// `code` when the component keeps it elsewhere) does nothing and the node
// runs as a no-op — a green build that fails at runtime. Returns "" when
// everything matches, when there are no settings, or when the schema can't
// be resolved to a closed set of named fields (open/opaque schemas are
// never guess-flagged, to avoid false positives).
func unknownSettings(alias string, settings map[string]interface{}, schemaBytes []byte) string {
	if len(settings) == 0 || len(schemaBytes) == 0 {
		return ""
	}
	props := settingsProps(schemaBytes)
	if len(props) == 0 {
		return ""
	}
	var unknown []string
	for k := range settings {
		if _, ok := props[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	sort.Strings(unknown)
	valid := make([]string, 0, len(props))
	for k := range props {
		valid = append(valid, k)
	}
	sort.Strings(valid)
	return fmt.Sprintf(
		"node '%s' sets unknown setting(s) %v — the component ignores settings it doesn't declare, so those fields silently do nothing. Valid settings: %v. Use these exact names (get_component_info reports the `settings` schema).",
		alias, unknown, valid,
	)
}

// settingsProps resolves a _settings JSON schema to its top-level setting
// properties (name → sub-schema), following a single $ref into $defs to the
// Settings object. Returns nil for open schemas (additionalProperties not
// false) so callers skip the unknown-field check on those.
func settingsProps(schemaBytes []byte) map[string]interface{} {
	var root map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &root); err != nil {
		return nil
	}
	node := root
	if ref, ok := root["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		if defs, ok := root["$defs"].(map[string]interface{}); ok {
			if t, ok := defs[name].(map[string]interface{}); ok {
				node = t
			}
		}
	}
	if ap, ok := node["additionalProperties"]; ok {
		if b, isBool := ap.(bool); !isBool || b {
			return nil // open schema — extra keys are allowed
		}
	}
	props, _ := node["properties"].(map[string]interface{})
	return props
}

// portSchemaBytes returns the raw JSON schema bytes for the named port
// on the given component, or nil if the catalog didn't carry it. Input
// ports and output ports live in different slices on ComponentInfo;
// pass isInput=true to look in InputPortDetails (which includes the
// `_settings` port), false for OutputPortDetails.
func portSchemaBytes(comp *ComponentInfo, portName string, isInput bool) []byte {
	if comp == nil {
		return nil
	}
	ports := comp.OutputPortDetails
	if isInput {
		ports = comp.InputPortDetails
	}
	for _, p := range ports {
		if p.Name == portName {
			return p.Schema
		}
	}
	return nil
}

// configurableFieldsIn scans a JSON Schema (as raw bytes) and returns the
// list of field names declared with `configurable: true`. Configurable
// fields are the variadic ones — their shape is supplied by the author
// alongside the data, not declared on the component's Go struct. We use
// this list to enforce that when an author supplies *data* for a
// configurable field, they MUST also supply a *schema* for it.
//
// Returns an empty slice (not nil) when no configurable fields are
// present, so callers can range over the result without nil checks.
func configurableFieldsIn(schemaBytes []byte) []string {
	if len(schemaBytes) == 0 {
		return []string{}
	}
	var root map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &root); err != nil {
		return []string{}
	}
	defs, ok := root["$defs"].(map[string]interface{})
	if !ok {
		return []string{}
	}
	seen := map[string]struct{}{}
	for defName, raw := range defs {
		def, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if configurable, _ := def["configurable"].(bool); !configurable {
			continue
		}
		// Translate the $defs name back to a field name. The platform
		// reflector capitalises just the first character ("context" →
		// "Context", "outputdata" → "Outputdata"). Lowercase the first
		// letter to recover the JSON field name.
		name := defName
		if name == "" {
			continue
		}
		seen[lowerFirstByte(name)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func lowerFirstByte(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}

// requireSchemaForData reports which configurable fields are filled in
// `data` without a matching entry in `userSchema`. The author must
// either provide a schema for those fields or omit the data — the
// platform no longer infers a schema from the value shape.
func requireSchemaForData(data, userSchema map[string]interface{}, configurable []string) []string {
	var missing []string
	for _, field := range configurable {
		if _, hasData := data[field]; !hasData {
			continue
		}
		if userSchema != nil {
			if _, hasSchema := userSchema[field]; hasSchema {
				continue
			}
		}
		// `context` is the ubiquitous opaque passthrough. When it is forwarded
		// as an expression (a string like "{{$.context}}" or "{{$}}") there is
		// no inlined shape to describe, so requiring a schema is pure friction —
		// the most-repeated papercut in flow authoring. A structured context (an
		// object/array literal that introduces new fields) still needs one.
		if field == "context" {
			if _, isString := data[field].(string); isString {
				continue
			}
		}
		missing = append(missing, field)
	}
	return missing
}

// fillMissingSchemas returns userSchema (copied) with a DERIVED entry for
// every filled configurable field that lacks one, so authors no longer pay
// the "declare a schema for context on every edge" tax. Derivation is
// resolution-based, not value-shape-based: the inference that was removed in
// 5208235 typed template strings as "string" regardless of what they resolve
// to, producing confidently-wrong schemas. Here a whole-string expression is
// typed by resolving it against the SOURCE (port schema + node example) via
// `resolve`; when resolution fails the property stays `{}` — untyped, never
// wrong — while literals keep their actual JSON type and concatenations are
// strings by construction. User-provided entries always win.
func fillMissingSchemas(data, userSchema map[string]interface{}, configurable []string, resolve func(string) interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range userSchema {
		out[k] = v
	}
	for _, field := range configurable {
		val, hasData := data[field]
		if !hasData {
			continue
		}
		if _, hasSchema := out[field]; hasSchema {
			continue
		}
		// String passthrough ("{{$}}" / "{{$.context}}") stays exempt — no
		// inlined shape to describe.
		if _, isString := val.(string); isString && field == "context" {
			continue
		}
		out[field] = deriveValueSchema(val, resolve)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// deriveValueSchema builds a JSON-schema fragment for a configurable-field
// value. See fillMissingSchemas for the typing rules.
func deriveValueSchema(v interface{}, resolve func(string) interface{}) map[string]interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		props := map[string]interface{}{}
		for k, val := range x {
			props[k] = deriveValueSchema(val, resolve)
		}
		return map[string]interface{}{"type": "object", "properties": props}
	case []interface{}:
		s := map[string]interface{}{"type": "array"}
		if len(x) > 0 {
			s["items"] = deriveValueSchema(x[0], resolve)
		}
		return s
	case string:
		m := expressionRe.FindStringSubmatch(x)
		if m == nil {
			return map[string]interface{}{"type": "string"} // literal
		}
		if m[0] != x {
			return map[string]interface{}{"type": "string"} // concat — string by construction
		}
		// Whole-string expression: type = whatever it resolves to.
		if resolve != nil {
			for _, full := range jsonPathRe.FindAllString(m[1], -1) {
				path := strings.TrimPrefix(full, "$.")
				if path == "" || path == full {
					continue
				}
				if resolved := resolve(path); resolved != nil {
					return typeSchemaOf(resolved)
				}
			}
		}
		return map[string]interface{}{} // unresolvable — untyped, never wrong
	case bool:
		return map[string]interface{}{"type": "boolean"}
	case float64, int, int64:
		return map[string]interface{}{"type": "number"}
	default:
		return map[string]interface{}{}
	}
}

// typeSchemaOf maps a resolved example value to a schema fragment.
func typeSchemaOf(v interface{}) map[string]interface{} {
	switch x := v.(type) {
	case string:
		return map[string]interface{}{"type": "string"}
	case bool:
		return map[string]interface{}{"type": "boolean"}
	case float64, int, int64:
		return map[string]interface{}{"type": "number"}
	case []interface{}:
		s := map[string]interface{}{"type": "array"}
		if len(x) > 0 {
			s["items"] = typeSchemaOf(x[0])
		}
		return s
	case map[string]interface{}:
		props := map[string]interface{}{}
		for k, val := range x {
			props[k] = typeSchemaOf(val)
		}
		return map[string]interface{}{"type": "object", "properties": props}
	default:
		return map[string]interface{}{}
	}
}

// exampleResolver resolves a `$.a.b` path against a source port's schema
// (fixed typed fields, via typedPlaceholder) and then the source node's
// settings examples (shapeless fields like a js_eval outputData live there).
// Returns nil when neither knows the path.
func exampleResolver(sourcePortSchema []byte, sourceSettings map[string]interface{}) func(string) interface{} {
	return func(path string) interface{} {
		if v, ok := typedPlaceholder(sourcePortSchema, path); ok {
			return v
		}
		return walkExample(sourceSettings, path)
	}
}

// walkExample follows a dotted path (array suffixes → element 0) through a
// literal example structure.
func walkExample(example map[string]interface{}, path string) interface{} {
	if example == nil {
		return nil
	}
	var cur interface{} = example
	for _, raw := range strings.Split(path, ".") {
		m := segmentRe.FindStringSubmatch(raw)
		if m == nil {
			return nil
		}
		obj, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur, ok = obj[m[1]]
		if !ok {
			return nil
		}
		if m[2] != "" {
			arr, ok := cur.([]interface{})
			if !ok || len(arr) == 0 {
				return nil
			}
			cur = arr[0]
		}
	}
	return cur
}

// schemaRequiredError builds a uniform error message listing the fields
// missing a schema declaration, with the configurable-field convention
// spelled out so the model can self-correct.
func schemaRequiredError(kind, owner string, fields []string) error {
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%s '%s' fills configurable field(s) %v without a schema. "+
			"Declare each field's shape in the schema parameter — the platform "+
			"no longer infers schemas from value shape.",
		kind, owner, fields,
	)
}
