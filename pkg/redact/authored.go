package redact

import (
	"encoding/json"
	"strings"
)

// Redaction from what the USER declared, rather than from what a component did.
//
// Declared() covers a component's own fields, where the declaration is a Go
// struct tag. It cannot cover the case the design actually leads with:
// credentials reach a flow as port DATA, so they arrive inside a field marked
// `configurable`, whose shape the user authored — a map behind a `Context any`,
// with no struct tag anywhere near it. Reflection finds nothing to read.
//
// The authored schema IS the declaration for those values. A field the user
// marked `format:"password"` is shown to them as dots; it should not be written
// into a trace either. This reads that schema.
//
// Matching is by NAME, not by path, and that is deliberate. A path is only
// stable until an edge remaps it: the canonical settings-form flow declares
// `apiKey` on the form and its very next edge carries the value as
// `context.key`, so a path-exact rule would protect the first hop and leak on
// the second. The name travels further than the path does.
//
// Residual, stated plainly rather than papered over: a credential that an edge
// renames to something never declared anywhere is beyond this — it falls back to
// the name heuristic in Secrets and the value shapes in TextByShape. Declaring
// the field on both ends is what closes it.

// AuthoredSecretNames collects every property name a JSON Schema declares as a
// credential.
//
// It walks the whole document rather than resolving $ref, which sidesteps the
// SDK's title-casing of $def keys ("OutputData" becomes "Outputdata", so exact
// lookups miss). A declaration counts wherever it sits — inline, under
// properties, or inside $defs.
//
// Malformed or absent schemas yield nothing: this runs on live traffic and must
// never be the reason a hop fails.
func AuthoredSecretNames(schema []byte) map[string]bool {
	if len(schema) == 0 {
		return nil
	}
	var doc any
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil
	}
	out := map[string]bool{}
	collectSecretNames(doc, out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectSecretNames finds `properties` objects and records the names whose
// definition declares a secret.
func collectSecretNames(node any, out map[string]bool) {
	switch n := node.(type) {
	case map[string]any:
		if props, ok := n["properties"].(map[string]any); ok {
			for name, def := range props {
				if d, ok := def.(map[string]any); ok && declaresSecret(d) {
					out[name] = true
				}
			}
		}
		for _, v := range n {
			collectSecretNames(v, out)
		}
	case []any:
		for _, v := range n {
			collectSecretNames(v, out)
		}
	}
}

// declaresSecret reads the same three attributes isSecretField reads off a Go
// tag, so a component and a user declare a credential the same way.
func declaresSecret(def map[string]any) bool {
	if s, ok := def["format"].(string); ok && strings.EqualFold(s, "password") {
		return true
	}
	if b, ok := def["writeOnly"].(bool); ok && b {
		return true
	}
	if b, ok := def["secret"].(bool); ok && b {
		return true
	}
	// Tolerate string spellings — hand-written schemas and older exports carry
	// "true" rather than true, and a declaration that reads as intended to a
	// person should not fail silently.
	if s, ok := def["writeOnly"].(string); ok && strings.EqualFold(s, "true") {
		return true
	}
	if s, ok := def["secret"].(string); ok && strings.EqualFold(s, "true") {
		return true
	}
	return false
}

// ByName replaces string values sitting under any of the given names, at any
// depth. Returns the input untouched when nothing is declared or nothing
// matches, so the common payload costs one map lookup and no copy.
//
// Empty values and expressions are left alone, matching Secrets: there is
// nothing to hide in a blank, and an expression is a reference whose rewriting
// would break the wiring that depends on it.
func ByName(v any, names map[string]bool) (any, bool) {
	if len(names) == 0 || v == nil {
		return v, false
	}
	out, changed := byName("", v, names)
	if !changed {
		return v, false
	}
	return out, true
}

func byName(key string, v any, names map[string]bool) (any, bool) {
	switch n := v.(type) {
	case string:
		if !names[key] || n == "" || IsExpression(n) {
			return v, false
		}
		return Value, true

	case map[string]any:
		var copied map[string]any
		for k, child := range n {
			nv, changed := byName(k, child, names)
			if !changed {
				continue
			}
			if copied == nil {
				copied = make(map[string]any, len(n))
				for ck, cv := range n {
					copied[ck] = cv
				}
			}
			copied[k] = nv
		}
		if copied == nil {
			return v, false
		}
		return copied, true

	case []any:
		var copied []any
		for i, child := range n {
			// A slice element inherits the key its container sat under, so
			// headers[0].authorization is reached and a bare list of strings
			// under a declared name is redacted too.
			nv, changed := byName(key, child, names)
			if !changed {
				continue
			}
			if copied == nil {
				copied = make([]any, len(n))
				copy(copied, n)
			}
			copied[i] = nv
		}
		if copied == nil {
			return v, false
		}
		return copied, true
	}
	return v, false
}
