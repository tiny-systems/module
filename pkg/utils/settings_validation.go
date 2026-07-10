package utils

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SettingsIssues reports settings a component declared REQUIRED but whose
// value carries no schema/example — the "you must fill this" faults a graph
// author (human or model) skips. It is scoped to CONFIGURABLE required fields
// on purpose: those are the schema-carriers whose whole job is to let the
// platform type-check downstream wiring (a code component's output-shape
// field is the archetype — declare it and a wrong path downstream is caught
// at build time; leave it empty and every edge out of the node is
// unverifiable). Plain required scalars (a required bool, a required script)
// are left to normal validation + defaults, so this never floods a flow with
// false positives over fields that legitimately ride a default.
//
// Driven entirely by the component's own `required` + `configurable` tags —
// no component is named here. settingsSchema is the node's _settings port
// JSON Schema; settingsConfig is its configured value. Empty slice = clean.
func SettingsIssues(settingsSchema, settingsConfig []byte) []string {
	if len(settingsSchema) == 0 {
		return nil
	}
	var schema map[string]any
	if json.Unmarshal(settingsSchema, &schema) != nil {
		return nil
	}
	props, required := settingsShape(schema)
	if len(props) == 0 || len(required) == 0 {
		return nil
	}

	var config map[string]any
	if len(settingsConfig) > 0 {
		_ = json.Unmarshal(settingsConfig, &config)
	}

	var issues []string
	for _, name := range required {
		// `context` is the SDK's universal message-passthrough — configurable
		// and often marked required by a component, but legitimately blank
		// (it IS the widget form / the data that flows through at runtime),
		// never an author-declared output shape. Flagging it would fire on
		// almost every node, so it's excluded by framework convention (not a
		// component name — `context` is a first-class SDK primitive, Pattern
		// A/B in the core guide).
		if strings.EqualFold(name, "context") {
			continue
		}
		prop, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		// The configurable flag + title/description sit on the property OR,
		// when the reflector emitted a $ref (an `any`-typed field points at a
		// generated def like Outputdata), on that def. Merge both so we read
		// them wherever they landed.
		def := prop
		if resolved := resolveLocalRef(schema, stringField(prop, "$ref")); resolved != nil {
			def = resolved
		}
		if !isConfigurable(def) {
			continue // only schema-carrying fields; scalars ride defaults
		}
		if !isBlankValue(config[name]) {
			continue // author supplied a shape/example — good
		}
		title := firstNonEmpty(stringField(def, "title"), stringField(prop, "title"), name)
		msg := fmt.Sprintf("REQUIRED SETTING NOT SET: `%s` on this node has no schema/example.", title)
		if d := firstNonEmpty(stringField(def, "description"), stringField(prop, "description")); d != "" {
			msg += " " + d
		}
		issues = append(issues, msg)
	}
	return issues
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// settingsShape returns the Settings object's properties + required list,
// following a top-level $ref into $defs when the reflector emitted one
// (the common shape: {"$ref":"#/$defs/Settings","$defs":{"Settings":{...}}}).
func settingsShape(schema map[string]any) (props map[string]any, required []string) {
	obj := schema
	if ref := stringField(schema, "$ref"); ref != "" {
		if resolved := resolveLocalRef(schema, ref); resolved != nil {
			obj = resolved
		}
	}
	props, _ = obj["properties"].(map[string]any)
	for _, r := range asAnySlice(obj["required"]) {
		if s, ok := r.(string); ok {
			required = append(required, s)
		}
	}
	return props, required
}

// resolveLocalRef follows a "#/$defs/Name" pointer within the same document.
func resolveLocalRef(root map[string]any, ref string) map[string]any {
	const p = "#/$defs/"
	if !strings.HasPrefix(ref, p) {
		return nil
	}
	defs, _ := root["$defs"].(map[string]any)
	target, _ := defs[strings.TrimPrefix(ref, p)].(map[string]any)
	return target
}

// isConfigurable reports whether a property is a configurable schema-carrier.
// The reflector marks the property itself `configurable: true`.
func isConfigurable(prop map[string]any) bool {
	b, _ := prop["configurable"].(bool)
	return b
}

// isBlankValue reports whether a settings value carries no real content:
// absent, null, empty string, empty object, or empty array. A zero number or
// false bool is NOT blank — it's a deliberate value.
func isBlankValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	default:
		return false
	}
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func asAnySlice(v any) []any {
	s, _ := v.([]any)
	return s
}
