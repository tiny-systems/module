// Package redact hides credential-shaped values in data that is about to be
// stored, displayed, or published.
//
// It lives in its own leaf package so components can use it without pulling
// in pkg/tools (which drags the whole agent-tooling and Kubernetes surface
// into every module binary).
package redact

import (
	"regexp"
	"strings"
)

// Value replaces a string that sat under a credential-shaped key.
const Value = "<redacted>"

// secretKeyRe matches map keys that conventionally hold credentials. Matching
// is by key name, not value shape — payloads carry no schema at this point,
// so the field name is the only signal available.
var secretKeyRe = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|passw|authorization|bearer|credential|private[_-]?key|access[_-]?key)`)

// IsSecretKey reports whether a field name conventionally holds a credential.
func IsSecretKey(key string) bool {
	return key != "" && secretKeyRe.MatchString(key)
}

// IsExpression reports whether a value is a template reference rather than
// literal data. An expression names a secret; it does not contain one, and
// rewriting it severs whatever wiring depends on it.
func IsExpression(s string) bool {
	return strings.Contains(s, "{{")
}

// Secrets walks a decoded JSON value and replaces string values whose key
// looks credential-shaped. Containers are recursed regardless of their own
// key, so context.apiKey and headers[0].authorization are both caught. The
// walk copies — the input is never mutated.
//
// Empty values and expressions are left alone: there is nothing to hide in
// either, and replacing them turns a blank slot or a live reference into a
// literal marker.
func Secrets(v interface{}) interface{} {
	return value("", v)
}

func value(key string, v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[k] = value(k, val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = value(key, val)
		}
		return out
	case string:
		if x != "" && !IsExpression(x) && IsSecretKey(key) {
			return Value
		}
		return x
	default:
		return v
	}
}
