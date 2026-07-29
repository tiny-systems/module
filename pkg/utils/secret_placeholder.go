package utils

import "regexp"

// secretFieldRe matches field names that hold a credential. Kept in step with
// the identically-purposed pattern in pkg/tools/scenario_capture.go, which uses
// it to redact captured data; pkg/tools imports this package, so the two cannot
// share one definition without a cycle.
var secretFieldRe = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|passw|authorization|bearer|credential|private[_-]?key|access[_-]?key)`)

// simulatedSecretPlaceholder stands in for a credential during validation. It
// is deliberately not credential-shaped: nothing may mistake it for a usable
// value if it ever surfaces in a message or a log.
const simulatedSecretPlaceholder = "(supplied at runtime)"

// placeholderForAbsentSecrets replaces null credential fields with a
// placeholder string, recursively.
//
// Why null and not simply mocked like other gaps: a credential is not missing
// because the simulator failed to reach it, but because it does not exist until
// the flow runs — the user types it into the trigger widget. Validation would
// report `expected string, but got null` and mark a correct edge as broken. A
// placeholder keeps the field's type honest so every other field is still
// checked, rather than demoting the whole edge to unverifiable.
//
// Only nulls are touched. A credential the flow really does carry keeps its
// value and is validated normally, so a genuine type error is still caught.
func placeholderForAbsentSecrets(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if val == nil && secretFieldRe.MatchString(key) {
				v[key] = simulatedSecretPlaceholder
				continue
			}
			v[key] = placeholderForAbsentSecrets(val)
		}
		return v
	case []interface{}:
		for i, val := range v {
			v[i] = placeholderForAbsentSecrets(val)
		}
		return v
	default:
		return data
	}
}
