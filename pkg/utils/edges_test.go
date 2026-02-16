package utils

import (
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
)

func TestReplaceFlowEdges(t *testing.T) {
	existing := []v1alpha1.TinyNodeEdge{
		{ID: "e1", FlowID: "flow-a"},
		{ID: "e2", FlowID: "flow-b"},
		{ID: "e3", FlowID: "flow-a"},
	}
	flowIDs := map[string]bool{"flow-a": true}
	newEdges := []v1alpha1.TinyNodeEdge{
		{ID: "e4", FlowID: "flow-a"},
	}

	result := ReplaceFlowEdges(existing, flowIDs, newEdges)

	// Should keep e2 (flow-b) and add e4 (new)
	if len(result) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(result))
	}
	if result[0].ID != "e2" {
		t.Errorf("expected first edge e2, got %s", result[0].ID)
	}
	if result[1].ID != "e4" {
		t.Errorf("expected second edge e4, got %s", result[1].ID)
	}
}

func TestReplaceFlowEdges_EmptyFlowIDs(t *testing.T) {
	existing := []v1alpha1.TinyNodeEdge{
		{ID: "e1", FlowID: "flow-a"},
	}
	newEdges := []v1alpha1.TinyNodeEdge{
		{ID: "e2", FlowID: "flow-b"},
	}

	result := ReplaceFlowEdges(existing, map[string]bool{}, newEdges)

	if len(result) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(result))
	}
}

func TestReplaceFlowPortConfigs(t *testing.T) {
	existing := []v1alpha1.TinyNodePortConfig{
		{Port: "_settings", From: "", FlowID: ""},                       // handle-level, keep
		{Port: "input", From: "nodeA:out", FlowID: "flow-a"},           // edge, flow-a, remove
		{Port: "input", From: "nodeB:out", FlowID: "flow-b"},           // edge, flow-b, keep
		{Port: "request", From: "nodeC:response", FlowID: "flow-a"},    // edge, flow-a, remove
	}
	flowIDs := map[string]bool{"flow-a": true}
	newConfigs := []v1alpha1.TinyNodePortConfig{
		{Port: "input", From: "nodeD:out", FlowID: "flow-a"},
	}

	result := ReplaceFlowPortConfigs(existing, flowIDs, newConfigs)

	// Should keep handle-level + flow-b edge + new
	if len(result) != 3 {
		t.Fatalf("expected 3 port configs, got %d", len(result))
	}
	if result[0].Port != "_settings" {
		t.Errorf("expected first to be handle-level _settings, got %s", result[0].Port)
	}
	if result[1].From != "nodeB:out" {
		t.Errorf("expected second to be flow-b edge, got from=%s", result[1].From)
	}
	if result[2].From != "nodeD:out" {
		t.Errorf("expected third to be new config, got from=%s", result[2].From)
	}
}

func TestRemoveNodeReferences(t *testing.T) {
	node := &v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "e1", To: "deletedNode:input"},
				{ID: "e2", To: "otherNode:input"},
				{ID: "e3", To: "deletedNode:request"},
			},
			Ports: []v1alpha1.TinyNodePortConfig{
				{Port: "_settings", From: ""},                      // handle-level, keep
				{Port: "input", From: "deletedNode:out"},           // from deleted, remove
				{Port: "request", From: "otherNode:response"},      // from other, keep
			},
		},
	}

	changed := RemoveNodeReferences(node, "deletedNode")

	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(node.Spec.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(node.Spec.Edges))
	}
	if node.Spec.Edges[0].ID != "e2" {
		t.Errorf("expected edge e2, got %s", node.Spec.Edges[0].ID)
	}
	if len(node.Spec.Ports) != 2 {
		t.Fatalf("expected 2 port configs, got %d", len(node.Spec.Ports))
	}
	if node.Spec.Ports[0].Port != "_settings" {
		t.Errorf("expected first port _settings, got %s", node.Spec.Ports[0].Port)
	}
	if node.Spec.Ports[1].From != "otherNode:response" {
		t.Errorf("expected second port from otherNode, got %s", node.Spec.Ports[1].From)
	}
}

func TestRemoveNodeReferences_NoChange(t *testing.T) {
	node := &v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "e1", To: "otherNode:input"},
			},
			Ports: []v1alpha1.TinyNodePortConfig{
				{Port: "input", From: "otherNode:out"},
			},
		},
	}

	changed := RemoveNodeReferences(node, "deletedNode")

	if changed {
		t.Fatal("expected changed=false")
	}
	if len(node.Spec.Edges) != 1 {
		t.Fatalf("expected 1 edge unchanged, got %d", len(node.Spec.Edges))
	}
}
