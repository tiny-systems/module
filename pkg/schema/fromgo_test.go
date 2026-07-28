package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

type ctrl struct {
	Approve bool   `json:"approve" format:"button" title:"Approve"`
	Note    string `json:"note" title:"Note"`
}

func TestFromGoProducesUsableSchema(t *testing.T) {
	raw := FromGo(ctrl{})
	if raw == nil {
		t.Fatal("FromGo returned nil for a valid value")
	}
	if !json.Valid(raw) {
		t.Fatalf("FromGo produced invalid JSON: %s", raw)
	}
	// The custom render attributes a form depends on must survive.
	if !strings.Contains(string(raw), `"button"`) {
		t.Errorf("format:button did not survive into the schema: %s", raw)
	}
}

func TestFromGoNilValueIsEmptySchema(t *testing.T) {
	// CreateSchema treats nil as the empty schema; FromGo must not report that
	// as failure, or a component publishing an empty form would silently fall
	// back to reflection.
	raw := FromGo(nil)
	if raw == nil {
		t.Fatal("FromGo(nil) returned nil; expected the empty schema")
	}
	if !json.Valid(raw) {
		t.Fatalf("FromGo(nil) produced invalid JSON: %s", raw)
	}
}

// A runtime-authored form has no Go type at all — it arrives as bytes and is
// assigned straight to Port.Schema. Decoding it into a generic map and back
// must not lose the render attributes, which is what the ask component relies
// on when it forwards a form it received as data.
func TestRuntimeAuthoredFormSurvivesRoundTrip(t *testing.T) {
	form := []byte(`{"type":"object","properties":{"approve":{"type":"boolean","format":"button","title":"Approve"}}}`)

	var decoded map[string]interface{}
	if err := json.Unmarshal(form, &decoded); err != nil {
		t.Fatalf("runtime form did not decode: %v", err)
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("runtime form did not re-encode: %v", err)
	}
	if !strings.Contains(string(reencoded), `"format":"button"`) {
		t.Errorf("format:button lost on round trip: %s", reencoded)
	}
}
