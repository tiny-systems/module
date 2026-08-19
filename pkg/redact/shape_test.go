package redact

import (
	"strings"
	"testing"
)

// The shape actually found in a live project: a user-supplied key, entered
// through a widget exactly as intended, captured back into a scenario sample
// by a key-value node's query_result. The field is called "value" — nothing
// about the name says secret, which is why key-name redaction misses it.
func TestJSONByShape_MasksCapturedKey(t *testing.T) {
	key := "sk-ant-api03-" + strings.Repeat("A1b2C3d4", 11)
	data := []byte(`{"context":{"id":7},"value":"` + key + `"}`)

	out, changed := JSONByShape(data)
	if !changed {
		t.Fatal("key was not detected")
	}
	if strings.Contains(string(out), key) {
		t.Fatalf("key survived: %s", out)
	}
	if !strings.Contains(string(out), Value) {
		t.Fatalf("no redaction marker: %s", out)
	}
	// The payload's shape has to survive — samples exist to pin it.
	if !strings.Contains(string(out), `"context"`) || !strings.Contains(string(out), `"value"`) {
		t.Fatalf("shape damaged: %s", out)
	}
}

// A log line carries a truncated key far more often than a whole one. The
// fragment is unusable alone but is still the leading portion of a real
// credential.
func TestJSONByShape_MasksTruncatedKey(t *testing.T) {
	out, changed := JSONByShape([]byte(`{"logs":"using key sk-ant-api03-Ab… (truncated)"}`))
	if !changed || strings.Contains(string(out), "sk-ant-") {
		t.Fatalf("prefix survived: %s", out)
	}
}

func TestJSONByShape_MasksInsideFreeText(t *testing.T) {
	data := []byte(`{"logs":"level=info msg=\"calling api\" token=ghp_` + strings.Repeat("z", 30) + `"}`)
	out, changed := JSONByShape(data)
	if !changed || strings.Contains(string(out), "ghp_zzzz") {
		t.Fatalf("token survived: %s", out)
	}
}

// A false positive silently corrupts data someone relies on, so the common
// random-looking identifiers must round-trip byte for byte.
func TestJSONByShape_LeavesOrdinaryDataAlone(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"name":"broken-checkout-657f5f7dd-6mz5c","restarts":0}`),
		[]byte(`{"id":"3802e363-9bc2-11f1-9d40-763d6350ade8"}`),
		[]byte(`{"sha":"b83f5420419d8af18f2c2b66090520f7aff145bb"}`),
		[]byte(`{"maxBytes":9007199254740993}`),
	} {
		out, changed := JSONByShape(data)
		if changed {
			t.Fatalf("rewrote ordinary data: %s -> %s", data, out)
		}
		if string(out) != string(data) {
			t.Fatalf("not byte-identical: %s -> %s", data, out)
		}
	}
}

// Non-JSON still must not carry a credential out.
func TestJSONByShape_ScrubsNonJSON(t *testing.T) {
	data := []byte("Authorization: Bearer " + strings.Repeat("k", 40))
	out, changed := JSONByShape(data)
	if !changed || strings.Contains(string(out), strings.Repeat("k", 40)) {
		t.Fatalf("bearer token survived: %s", out)
	}
}

// Nested and array positions are reached, not just top-level fields.
func TestSecretsByShape_ReachesNestedPositions(t *testing.T) {
	v := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"note": "key AKIA" + strings.Repeat("Q", 16)},
		},
	}
	_, changed := SecretsByShape(v)
	if !changed {
		t.Fatal("nested credential not reached")
	}
	if strings.Contains(v["items"].([]interface{})[0].(map[string]interface{})["note"].(string), "AKIA") {
		t.Fatal("nested credential survived")
	}
}

// This is the complement of key-name redaction, not a replacement — the two
// catch different things, and both are needed.
func TestByShapeComplementsByKeyName(t *testing.T) {
	// Named field, innocuous-looking value: only IsSecretKey catches it.
	if _, changed := JSONByShape([]byte(`{"apiKey":"hunter2"}`)); changed {
		t.Fatal("shape matching should not claim this one")
	}
	if !IsSecretKey("apiKey") {
		t.Fatal("key-name matching should claim it")
	}

	// Innocuous name, real key: only shape catches it.
	if !IsSecretKey("value") {
		if _, changed := JSONByShape([]byte(`{"value":"sk-ant-api03-` + strings.Repeat("z", 40) + `"}`)); !changed {
			t.Fatal("shape matching should claim this one")
		}
	}
}
