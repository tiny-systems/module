package tools

import (
	"strings"
	"testing"
)

func TestFirstSentence_KeepsShortNotesWhole(t *testing.T) {
	// Cutting a note that was already brief buys nothing and can leave it
	// saying less than the component's own name.
	short := "Get an array element by 1-based index."
	if got := firstSentence(short); got != short {
		t.Fatalf("got %q, want it untouched", got)
	}
}

func TestFirstSentence_CutsAtTheFirstRealSentenceEnd(t *testing.T) {
	info := "Holds a value in a key-value store and returns it on query. " +
		"Wire the store port to write and the query port to read; the document id is the key, " +
		"and a missing key answers on the error port rather than failing the flow."

	got := firstSentence(info)
	if got != "Holds a value in a key-value store and returns it on query." {
		t.Fatalf("got %q", got)
	}
	if len(got) >= len(info) {
		t.Fatal("nothing was saved")
	}
}

// A decimal point or an abbreviation is not a sentence ending. Cutting there
// produces a fragment that reads as a mistake.
func TestFirstSentence_IgnoresNonSentenceDots(t *testing.T) {
	info := "Waits for 1.5 seconds by default before forwarding the message downstream, " +
		"which is enough to let a slow consumer catch up. Configure the delay in settings."

	got := firstSentence(info)
	if strings.Contains(got, "Waits for 1.") && !strings.Contains(got, "1.5 seconds") {
		t.Fatalf("cut at a decimal point: %q", got)
	}
	if !strings.HasSuffix(got, "catch up.") {
		t.Fatalf("got %q, want the cut at the real sentence end", got)
	}
}

func TestFirstSentence_HandlesNoteWithNoSentenceEnd(t *testing.T) {
	info := strings.Repeat("a note that never ends ", 12)
	if got := firstSentence(info); got != strings.TrimSpace(info) {
		t.Fatalf("a note with no sentence end must survive whole, got %d chars", len(got))
	}
}

// System ports are not wireable, and get_component_info has always hidden
// them. Listing them here only invited an agent to try.
func TestWireablePorts_DropsSystemPorts(t *testing.T) {
	got := wireablePorts([]string{"_settings", "request", "_control", "response", "_reconcile", "_client", "_identity"})
	if len(got) != 2 || got[0] != "request" || got[1] != "response" {
		t.Fatalf("got %v, want [request response]", got)
	}
}

func TestWireablePorts_KeepsOrdinaryPortsIntact(t *testing.T) {
	in := []string{"request", "response", "error"}
	got := wireablePorts(in)
	if len(got) != len(in) {
		t.Fatalf("got %v, want %v", got, in)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Fatalf("order changed: %v vs %v", got, in)
		}
	}
}

// The case this exists for. An agent picked pod_status_get off a catalog line
// reading "get status of pods matching a label selector" — which sounds like a
// capability and is in fact an obligation, making the component unusable for
// "all pods in a namespace". The constraint lives on the request port.
func TestRequiredInputs_NamesAMandatoryPortField(t *testing.T) {
	c := ComponentInfo{
		InputPortDetails: []PortDetail{{
			Name: "request",
			Schema: []byte(`{"$ref":"#/$defs/Request","$defs":{"Request":{
				"properties":{"labelSelector":{"type":"string","minLength":3},"namespace":{"type":"string"}},
				"required":["labelSelector"]}}}`),
		}},
	}
	got := requiredInputs(c)
	if len(got) != 1 || got[0] != "request.labelSelector" {
		t.Fatalf("got %v, want [request.labelSelector]", got)
	}
}

// A boolean toggle is never the thing that blocks you — false is a usable
// value — and listing it buries the field that does.
func TestRequiredInputs_SkipsTogglesAndDefaultedFields(t *testing.T) {
	c := ComponentInfo{
		SettingsSchema: []byte(`{"properties":{
			"enableErrorPort":{"type":"boolean"},
			"timeout":{"type":"integer","default":30},
			"provider":{"type":"string"}},
			"required":["enableErrorPort","timeout","provider"]}`),
	}
	got := requiredInputs(c)
	if len(got) != 1 || got[0] != "settings.provider" {
		t.Fatalf("got %v, want [settings.provider]", got)
	}
}

// Settings and ports are both places a caller must supply something, and the
// path says which.
func TestRequiredInputs_CoversSettingsAndPortsTogether(t *testing.T) {
	c := ComponentInfo{
		SettingsSchema: []byte(`{"properties":{"document":{"type":"object"}},"required":["document"]}`),
		InputPortDetails: []PortDetail{
			{Name: "_settings", Schema: []byte(`{"properties":{"hidden":{"type":"string"}},"required":["hidden"]}`)},
			{Name: "query", Schema: []byte(`{"properties":{"query":{"type":"string"}},"required":["query"]}`)},
		},
	}
	got := requiredInputs(c)
	if len(got) != 2 || got[0] != "settings.document" || got[1] != "query.query" {
		t.Fatalf("got %v, want [settings.document query.query]", got)
	}
}

// A component with nothing mandatory says nothing, rather than an empty list
// the reader has to interpret.
func TestRequiredInputs_SilentWhenNothingIsMandatory(t *testing.T) {
	c := ComponentInfo{
		InputPortDetails: []PortDetail{{Name: "in", Schema: []byte(`{"properties":{"text":{"type":"string"}}}`)}},
	}
	if got := requiredInputs(c); got != nil {
		t.Fatalf("got %v, want nothing", got)
	}
}

func TestRequiredInputs_SurvivesUnparseableSchemas(t *testing.T) {
	c := ComponentInfo{
		SettingsSchema:   []byte(`not json`),
		InputPortDetails: []PortDetail{{Name: "in", Schema: []byte(`{"$ref":"#/$defs/Missing","$defs":{}}`)}},
	}
	if got := requiredInputs(c); got != nil {
		t.Fatalf("got %v, want nothing", got)
	}
}

// The fact that separates two components which sound alike: one hands back
// the pods, the other hands back counters bucketed by phase. Choosing between
// them was impossible without fetching both in full.
func TestOutputShapes_NamesWhatAPortAnswersWith(t *testing.T) {
	c := ComponentInfo{
		OutputPortDetails: []PortDetail{{
			Name:   "result",
			Schema: []byte(`{"properties":{"pods":{"type":"array"},"count":{"type":"integer"},"context":{"type":"object"}}}`),
		}},
	}
	got := outputShapes(c)
	fields := got["result"]
	if len(fields) != 2 || fields[0] != "count" || fields[1] != "pods[]" {
		t.Fatalf("got %v, want [count pods[]]", fields)
	}
	// An array is the difference between "the things" and "a number about
	// the things", so it has to be visible.
	if fields[1] != "pods[]" {
		t.Fatal("array marker lost")
	}
}

// Context rides nearly every port, so naming it discriminates nothing while
// costing something on every component in the catalog.
func TestOutputShapes_OmitsThePassthroughContext(t *testing.T) {
	c := ComponentInfo{
		OutputPortDetails: []PortDetail{{
			Name:   "out",
			Schema: []byte(`{"properties":{"context":{"type":"object"}}}`),
		}},
	}
	if got := outputShapes(c); got != nil {
		t.Fatalf("got %v, want nothing — context alone is not a shape", got)
	}
}

func TestOutputShapes_SkipsSystemPortsAndEmptySchemas(t *testing.T) {
	c := ComponentInfo{
		OutputPortDetails: []PortDetail{
			{Name: "_control", Schema: []byte(`{"properties":{"text":{"type":"string"}}}`)},
			{Name: "out", Schema: nil},
		},
	}
	if got := outputShapes(c); got != nil {
		t.Fatalf("got %v, want nothing", got)
	}
}
