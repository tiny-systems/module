package redact

import (
	"testing"
)

// A credential the USER declared, travelling as port data.
//
// This is the normal case, not an edge case: credentials reach a flow as data
// and never as routing, so most of them live inside a `configurable` field
// whose shape the user authored. Declared() cannot see those — it reflects over
// Go struct tags, and the value is a map inside a `Context any`. TextByShape
// cannot either unless the value happens to look like a known provider's key.
//
// So the authored schema is the only place the declaration exists, and this is
// what reads it.
func TestAuthoredSecretNamesFromSchema(t *testing.T) {
	schema := []byte(`{
	  "type": "object",
	  "properties": {
	    "context": {
	      "type": "object",
	      "properties": {
	        "apiKey":   {"type": "string", "format": "password"},
	        "vpnPass":  {"type": "string", "writeOnly": true},
	        "legacy":   {"type": "string", "secret": true},
	        "region":   {"type": "string"}
	      }
	    }
	  }
	}`)

	got := AuthoredSecretNames(schema)
	for _, want := range []string{"apiKey", "vpnPass", "legacy"} {
		if !got[want] {
			t.Errorf("%q declared secret in the schema but not collected", want)
		}
	}
	if got["region"] || got["context"] {
		t.Errorf("collected a non-secret field: %v", got)
	}
}

// $defs and $ref are how the SDK actually writes schemas, and the $def keys are
// title-cased ("OutputData" -> "Outputdata"), so anything that tried to resolve
// a $ref by exact key would miss. Collecting names by walking the whole
// document sidesteps resolution entirely — a declaration is a declaration
// wherever it sits.
func TestAuthoredSecretNamesReachesInsideDefs(t *testing.T) {
	schema := []byte(`{
	  "$defs": {
	    "Context": {
	      "properties": {
	        "token": {"type": "string", "format": "password"},
	        "name":  {"type": "string"}
	      }
	    }
	  },
	  "properties": {"context": {"$ref": "#/$defs/Context"}}
	}`)

	got := AuthoredSecretNames(schema)
	if !got["token"] {
		t.Error("a secret declared inside $defs was not collected")
	}
	if got["name"] {
		t.Error("collected a non-secret from $defs")
	}
}

func TestAuthoredSecretNamesToleratesJunk(t *testing.T) {
	for _, in := range [][]byte{nil, {}, []byte("not json"), []byte(`{"properties":"a string"}`), []byte("null")} {
		if got := AuthoredSecretNames(in); len(got) != 0 {
			t.Errorf("AuthoredSecretNames(%q) = %v, want empty", in, got)
		}
	}
}

// The point of the whole exercise: a corporate password under a field name no
// regex would guess, declared by the user, is redacted.
func TestByNameRedactsWhatTheUserDeclared(t *testing.T) {
	payload := map[string]any{
		"context": map[string]any{
			"z":      "hunter2-corporate-vpn-password",
			"region": "eu-west-1",
		},
	}
	got, changed := ByName(payload, map[string]bool{"z": true})
	if !changed {
		t.Fatal("declared secret was not redacted")
	}
	inner := got.(map[string]any)["context"].(map[string]any)
	if inner["z"] != Value {
		t.Errorf("z = %v, want %s", inner["z"], Value)
	}
	if inner["region"] != "eu-west-1" {
		t.Errorf("region = %v, want it untouched", inner["region"])
	}
}

// Nothing declared must mean nothing copied — this runs on every hop of every
// flow, and most payloads carry no credential at all.
func TestByNameIsANoOpWithoutDeclarations(t *testing.T) {
	payload := map[string]any{"a": "b"}
	got, changed := ByName(payload, nil)
	if changed {
		t.Error("reported a change with no declared names")
	}
	if got.(map[string]any)["a"] != "b" {
		t.Error("payload was altered")
	}
}

// An expression names a credential, it does not contain one. Rewriting it would
// sever the wiring that depends on it — same rule the rest of the package
// follows.
func TestByNameLeavesExpressionsAndBlanksAlone(t *testing.T) {
	payload := map[string]any{
		"apiKey": "{{$.context.apiKey}}",
		"other":  "",
	}
	got, changed := ByName(payload, map[string]bool{"apiKey": true, "other": true})
	if changed {
		t.Error("rewrote an expression or a blank")
	}
	m := got.(map[string]any)
	if m["apiKey"] != "{{$.context.apiKey}}" {
		t.Errorf("apiKey = %v, want the expression intact", m["apiKey"])
	}
}

// Slices and nested containers, since a credential can sit in a list of headers.
func TestByNameWalksSlices(t *testing.T) {
	payload := map[string]any{
		"headers": []any{
			map[string]any{"xToken": "abc123"},
			map[string]any{"plain": "ok"},
		},
	}
	got, changed := ByName(payload, map[string]bool{"xToken": true})
	if !changed {
		t.Fatal("did not walk into a slice")
	}
	// Assert the values, not marshalled JSON: encoding/json HTML-escapes the
	// marker to \u003credacted\u003e, which is a quirk of the encoder and not
	// of the redaction (see shape.go, which handles the same thing).
	hs := got.(map[string]any)["headers"].([]any)
	if v := hs[0].(map[string]any)["xToken"]; v != Value {
		t.Errorf("headers[0].xToken = %v, want %s", v, Value)
	}
	if v := hs[1].(map[string]any)["plain"]; v != "ok" {
		t.Errorf("headers[1].plain = %v, want it untouched", v)
	}
}
