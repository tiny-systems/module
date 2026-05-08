package signalname_test

import (
	"strings"
	"testing"

	"github.com/tiny-systems/module/pkg/signalname"
)

const exampleNode = "e6f77e12.tinysystems-http-module-v0.http-server-ws01"

// TestFor_Deterministic — the entire reason the function exists. Same
// inputs must produce the same name so retries dedup naturally.
func TestFor_Deterministic(t *testing.T) {
	a := signalname.For(exampleNode, "request", []byte(`{"a":1}`), "edge-x", "parent-y")
	b := signalname.For(exampleNode, "request", []byte(`{"a":1}`), "edge-x", "parent-y")
	if a != b {
		t.Errorf("identical inputs produced different names: %q vs %q", a, b)
	}
}

// TestFor_PayloadChangesName — different message body must yield a
// different name; otherwise legitimate distinct messages would collide
// and dedup would lose data.
func TestFor_PayloadChangesName(t *testing.T) {
	a := signalname.For(exampleNode, "request", []byte(`{"v":1}`), "edge", "parent")
	b := signalname.For(exampleNode, "request", []byte(`{"v":2}`), "edge", "parent")
	if a == b {
		t.Errorf("different payloads produced same name: %q", a)
	}
}

// TestFor_ParentEdgeChangesName — multi-sender to the same target: each
// sender has its own parentEdgeID, so identical payloads from different
// senders produce different names. Both signals coexist; both get
// delivered.
func TestFor_ParentEdgeChangesName(t *testing.T) {
	payload := []byte(`{"shared":"payload"}`)
	a := signalname.For(exampleNode, "request", payload, "edge-from-A", "")
	b := signalname.For(exampleNode, "request", payload, "edge-from-B", "")
	if a == b {
		t.Errorf("different parentEdgeIDs produced same name: %q", a)
	}
}

// TestFor_ParentSignalUIDChangesName — chained signals from different
// parents must be distinct, otherwise a retried chain step could collide
// with another in-flight chain's step at the same logical position.
func TestFor_ParentSignalUIDChangesName(t *testing.T) {
	a := signalname.For(exampleNode, "request", []byte(`{}`), "edge", "parent-uid-1")
	b := signalname.For(exampleNode, "request", []byte(`{}`), "edge", "parent-uid-2")
	if a == b {
		t.Errorf("different parentSignalUIDs produced same name: %q", a)
	}
}

// TestFor_RFC1123Compliant — names go into K8s resource metadata, which
// constrains them to lowercase alphanumeric, dots, and hyphens, ≤253 chars.
// The function must never produce a name that K8s rejects.
func TestFor_RFC1123Compliant(t *testing.T) {
	cases := []struct {
		node string
		port string
	}{
		{exampleNode, "request"},
		{exampleNode, "_settings"},
		{exampleNode, "_control"},
		{exampleNode, "_reconcile"},
		{"WeirdMixedCase.Node-Name", "with_underscores"},
		{"node.with.lots.of.dots.and-hyphens-too", "p"},
	}
	for _, c := range cases {
		got := signalname.For(c.node, c.port, []byte(`{}`), "", "")
		if !isRFC1123(got) {
			t.Errorf("non-compliant name produced for node=%q port=%q: %q", c.node, c.port, got)
		}
		if len(got) > 253 {
			t.Errorf("name exceeds 253 chars (got %d): %q", len(got), got)
		}
	}
}

// TestFor_PortNameInOutput — the port is visible in the name so debugging
// (`kubectl get tinysignal | grep settings`) works. We don't assert exact
// substring match (sanitization may strip the leading underscore) but a
// recognizable form must be there.
func TestFor_PortNameInOutput(t *testing.T) {
	got := signalname.For(exampleNode, "request", []byte(`{}`), "", "")
	if !strings.Contains(got, "request") {
		t.Errorf("port name 'request' missing from generated name: %q", got)
	}
}

// TestFor_LongNodeNameTruncated — node names shouldn't push the result
// past 253 chars. Construct an unrealistically long node name and confirm.
func TestFor_LongNodeNameTruncated(t *testing.T) {
	longNode := strings.Repeat("a", 300)
	got := signalname.For(longNode, "p", []byte(`{}`), "", "")
	if len(got) > 253 {
		t.Errorf("long node name produced over-length result: len=%d", len(got))
	}
	// Digest must still be present at the end (truncation must come from
	// the prefix, never from the digest — otherwise we lose the signal
	// uniqueness guarantee).
	if !isHexSuffix(got, 12) {
		t.Errorf("truncated name lost its hex digest suffix: %q", got)
	}
}

// TestFor_EmptyParentsAreValid — externally-triggered signals (no parent
// edge, no parent signal) are common; the function must accept those.
func TestFor_EmptyParentsAreValid(t *testing.T) {
	got := signalname.For(exampleNode, "request", []byte(`{}`), "", "")
	if got == "" {
		t.Fatal("empty result for valid input")
	}
	if !isRFC1123(got) {
		t.Errorf("non-compliant name for empty parents: %q", got)
	}
}

// isRFC1123 reports whether s satisfies the RFC 1123 subset Kubernetes
// requires for resource names: nonempty, ≤253 chars, lowercase alphanumeric
// + dots + hyphens, must start and end with an alphanumeric.
func isRFC1123(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-'
		if !ok {
			return false
		}
		if (i == 0 || i == len(s)-1) && (r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// isHexSuffix reports whether s ends with at least n hex characters.
func isHexSuffix(s string, n int) bool {
	if len(s) < n {
		return false
	}
	for _, r := range s[len(s)-n:] {
		hex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !hex {
			return false
		}
	}
	return true
}
