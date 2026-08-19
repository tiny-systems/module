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
