// Package signalname computes deterministic names for TinySignal CRDs.
//
// The name is a function of the target node+port and the message contents
// (payload + parent edge ID + parent signal UID). Same inputs produce the
// same name, so:
//
//   - A cross-pod gRPC retry recreates the same signal name; the second
//     Create returns AlreadyExists, naturally deduping.
//
//   - Two distinct senders firing identical payloads to the same target
//     have different parent edge IDs and thus different names — both
//     signals exist independently, both get delivered (correct fan-in).
//
//   - A signal in a chain knows its parent's UID, so retried chain steps
//     don't collide with the original chain's signals.
//
// The chosen format is `sig-<node>-<port>-<12-hex-digest>`. The node and
// port are inlined (after sanitization) for human readability so
// `kubectl get tinysignals | grep <node>` works during incident response.
package signalname

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// digestLen is the truncated hash length appended to the name. 12 hex
// chars = 48 bits — collision resistance is fine for this scale (signals
// are short-lived; we'd need ~2^24 simultaneous messages to the same
// node:port pair to hit a 50% collision chance).
const digestLen = 12

// maxResourceName is the K8s upper bound for resource names (RFC 1123).
const maxResourceName = 253

// For computes a deterministic TinySignal name.
//
//   - node: the target TinyNode resource name (e.g.
//     "e6f77e12.tinysystems-http-module-v0.http-server-ws01")
//   - port: the target port (e.g. "request", "_settings")
//   - payload: the JSON-encoded message body (or whatever bytes the
//     edge will deliver)
//   - parentEdgeID: the edge that produced this message; empty for
//     externally-triggered signals (e.g. send_signal MCP, ticker fire)
//   - parentSignalUID: the parent TinySignal's UID for chain tracing;
//     empty when this signal isn't continuing an existing chain
//
// All five inputs are mixed into a sha256 hash; the first digestLen hex
// chars are appended to a sanitized "sig-<node>-<port>-" prefix.
func For(node, port string, payload []byte, parentEdgeID, parentSignalUID string) string {
	digest := digest(payload, parentEdgeID, parentSignalUID)
	prefix := "sig-" + sanitize(node) + "-" + sanitize(port) + "-"
	maxPrefix := maxResourceName - digestLen
	if len(prefix) > maxPrefix {
		prefix = prefix[:maxPrefix]
	}
	return prefix + digest
}

// digest returns digestLen hex chars of sha256(payload || 0 || parentEdgeID || 0 || parentSignalUID).
func digest(payload []byte, parentEdgeID, parentSignalUID string) string {
	h := sha256.New()
	_, _ = h.Write(payload)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(parentEdgeID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(parentSignalUID))
	return hex.EncodeToString(h.Sum(nil))[:digestLen]
}

// sanitize replaces any character that isn't a valid K8s resource-name
// character (lowercase alphanumeric, dot, hyphen) with '-'. Runs of
// hyphens collapse to one. Leading/trailing hyphens are trimmed.
//
// This is forgiving rather than strict: the result is always RFC 1123
// compliant for any input string.
func sanitize(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-'
		if !ok {
			r = '-'
		}
		if r == '-' && prevHyphen {
			continue
		}
		b.WriteRune(r)
		prevHyphen = r == '-'
	}
	return strings.Trim(b.String(), "-")
}
