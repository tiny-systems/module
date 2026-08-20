package utils

import "testing"

// A cloned solution's shared node names the flows it is shared into, and those
// names belong to the project it came from. Carried across unchanged they name
// flows that do not exist, so the node quietly fails to appear on the layers
// its author placed it on — the annotation is valid, the names are nobody's.
func TestResolveSharedFlows_RemapsToTheNewProjectsFlows(t *testing.T) {
	got := ResolveSharedFlows("watch,alerts", map[string]string{
		"watch":  "watch-9f2a1",
		"alerts": "alerts-3b8c4",
	})
	if got != "watch-9f2a1,alerts-3b8c4" {
		t.Fatalf("got %q, want both names remapped", got)
	}
}

// Joined with a bare comma: readers split on "," and match each segment
// exactly, so a space after the separator makes the name it precedes match
// nothing.
func TestResolveSharedFlows_TrimsAndJoinsWithoutSpaces(t *testing.T) {
	got := ResolveSharedFlows("watch, alerts", map[string]string{
		"watch":  "watch-9f2a1",
		"alerts": "alerts-3b8c4",
	})
	if got != "watch-9f2a1,alerts-3b8c4" {
		t.Fatalf("got %q — a space in the input must not survive", got)
	}
}

// A name with no mapping passes through rather than being dropped: losing it
// silently would un-share a node, which is a worse answer than leaving a name
// that can at least be recognised as wrong.
func TestResolveSharedFlows_KeepsUnmappedNames(t *testing.T) {
	got := ResolveSharedFlows("watch,stranger", map[string]string{"watch": "watch-9f2a1"})
	if got != "watch-9f2a1,stranger" {
		t.Fatalf("got %q, want the unmapped name kept", got)
	}
}
