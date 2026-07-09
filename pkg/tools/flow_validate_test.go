package tools

import (
	"strings"
	"testing"
)

func node(id, component string, outPorts ...string) map[string]interface{} {
	outs := make([]interface{}, len(outPorts))
	for i, p := range outPorts {
		outs[i] = p
	}
	return map[string]interface{}{
		"id":   id,
		"type": "tinyNode",
		"data": map[string]interface{}{
			"component": component,
			"ports":     map[string]interface{}{"out": outs},
		},
	}
}

func edge(id, source, sourceHandle, target string, edgeErr string) map[string]interface{} {
	data := map[string]interface{}{}
	if edgeErr != "" {
		data["error"] = edgeErr
	}
	return map[string]interface{}{
		"id":           id,
		"type":         "tinyEdge",
		"source":       source,
		"sourceHandle": sourceHandle,
		"target":       target,
		"data":         data,
	}
}

func TestFlowIssues_CleanFlow(t *testing.T) {
	els := []map[string]interface{}{
		node("sig", "signal", "out"),
		node("dbg", "debug"),
		edge("e1", "sig", "out", "dbg", ""),
	}
	if got := FlowIssues(els); len(got) != 0 {
		t.Fatalf("clean flow should have no issues, got %v", got)
	}
}

func TestFlowIssues_DanglingNode(t *testing.T) {
	els := []map[string]interface{}{
		node("sig", "signal", "out"),
		node("dbg", "debug"),
		node("orphan", "js_eval", "response"), // no edges at all
		edge("e1", "sig", "out", "dbg", ""),
	}
	got := FlowIssues(els)
	if len(got) != 1 || !strings.Contains(got[0], "DANGLING NODE orphan") {
		t.Fatalf("expected one dangling-node issue for orphan, got %v", got)
	}
	// A fully-dangling node must NOT also produce an error-port complaint.
	for _, iss := range got {
		if strings.Contains(iss, "UNCONNECTED ERROR PORT") {
			t.Fatalf("dangling node should not double-report an error port: %v", got)
		}
	}
}

func TestFlowIssues_UnconnectedErrorPort(t *testing.T) {
	els := []map[string]interface{}{
		node("sig", "signal", "out"),
		node("up", "vector_upsert", "response", "error"), // error port enabled, never wired
		edge("e1", "sig", "out", "up", ""),
	}
	got := FlowIssues(els)
	if len(got) != 1 || !strings.Contains(got[0], "UNCONNECTED ERROR PORT on up") {
		t.Fatalf("expected one unconnected-error-port issue, got %v", got)
	}
}

func TestFlowIssues_WiredErrorPortIsClean(t *testing.T) {
	els := []map[string]interface{}{
		node("sig", "signal", "out"),
		node("up", "vector_upsert", "response", "error"),
		node("dbg", "debug"),
		edge("e1", "sig", "out", "up", ""),
		edge("e2", "up", "error", "dbg", ""), // error port wired to debug
	}
	if got := FlowIssues(els); len(got) != 0 {
		t.Fatalf("wired error port should be clean, got %v", got)
	}
}

func TestFlowIssues_BrokenEdge(t *testing.T) {
	els := []map[string]interface{}{
		node("sig", "signal", "out"),
		node("dbg", "debug"),
		edge("e1", "sig", "out", "dbg", "'[' is not a constant"),
	}
	got := FlowIssues(els)
	if len(got) != 1 || !strings.Contains(got[0], "BROKEN EDGE e1") {
		t.Fatalf("expected one broken-edge issue, got %v", got)
	}
}
