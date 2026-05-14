package schema

import (
	"regexp"
)

// templatePattern matches "{{...}}" expressions used in edge configurations.
// A field whose value is a template at write-time will hold a string of
// whatever shape downstream — we can't see through the template, so the
// inferrer treats it as a plain string.
var templatePattern = regexp.MustCompile(`\{\{.*?\}\}`)

// InferFromInstance produces a JSON Schema describing the shape of a runtime
// value. It's a best-effort fallback for tool callers (typically LLMs) who
// supply data without an accompanying schema. The inferred schema is
// deliberately loose: it captures types, not constraints, and it doesn't
// guess at "required" because the value the caller sent IS the value, not
// a contract over multiple values.
//
// Inference rules:
//
//   - nil or absent → empty schema (callers should treat empty as "no
//     useful constraint").
//   - bool → {type: "boolean"}.
//   - integer-valued number → {type: "integer"}; non-integer → {type: "number"}.
//     (JSON unmarshalling gives us float64 for all numbers, so the
//     distinction is lost unless the caller passes a Go int directly.)
//   - string → {type: "string"}. Templates ("{{...}}") get the same
//     treatment — at write-time the value IS a string; downstream
//     validators may need an explicit schema if the rendered value will
//     be something else.
//   - map[string]any (or compatible) → {type: "object", properties: {...}}
//     with each property inferred recursively.
//   - []any (or compatible) → {type: "array", items: <inferred from first
//     element>} when non-empty, or {type: "array"} when empty.
//
// Callers that already passed a non-nil schema should NOT call this — the
// caller's schema always takes precedence.
func InferFromInstance(value any) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}

	switch v := value.(type) {
	case bool:
		return map[string]interface{}{"type": "boolean"}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return map[string]interface{}{"type": "integer"}
	case float32:
		return numberOrInteger(float64(v))
	case float64:
		return numberOrInteger(v)
	case string:
		return map[string]interface{}{"type": "string"}
	case map[string]interface{}:
		return inferObject(v)
	case []interface{}:
		return inferArray(v)
	default:
		// Unknown concrete type — leave empty so the validator doesn't
		// reject the field but doesn't make false claims either.
		return map[string]interface{}{}
	}
}

func numberOrInteger(f float64) map[string]interface{} {
	if f == float64(int64(f)) {
		return map[string]interface{}{"type": "integer"}
	}
	return map[string]interface{}{"type": "number"}
}

func inferObject(m map[string]interface{}) map[string]interface{} {
	props := make(map[string]interface{}, len(m))
	for k, v := range m {
		props[k] = InferFromInstance(v)
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
}

func inferArray(arr []interface{}) map[string]interface{} {
	out := map[string]interface{}{"type": "array"}
	if len(arr) == 0 {
		return out
	}
	// Use the first element's shape as the items schema. If the array is
	// heterogeneous the validator will reject — that's a real bug worth
	// surfacing, not something to paper over by guessing "any".
	out["items"] = InferFromInstance(arr[0])
	return out
}

// IsTemplate reports whether s contains a {{...}} expression. Exposed for
// callers that want to special-case template strings (e.g., relax type
// checks when the value will be templated at runtime).
func IsTemplate(s string) bool {
	return templatePattern.MatchString(s)
}
