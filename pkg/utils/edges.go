package utils

import "github.com/tiny-systems/module/api/v1alpha1"

// ReplaceFlowEdges removes all edges belonging to any of the specified flows,
// then appends newEdges. Used during import to prevent stale edges from
// accumulating when node IDs change across re-imports.
func ReplaceFlowEdges(existing []v1alpha1.TinyNodeEdge, flowIDs map[string]bool, newEdges []v1alpha1.TinyNodeEdge) []v1alpha1.TinyNodeEdge {
	clean := make([]v1alpha1.TinyNodeEdge, 0, len(existing))
	for _, e := range existing {
		if !flowIDs[e.FlowID] {
			clean = append(clean, e)
		}
	}
	return append(clean, newEdges...)
}

// ReplaceFlowPortConfigs removes edge-level port configs (From!="") belonging
// to any of the specified flows, then appends newConfigs. Handle-level configs
// (From=="") and configs from other flows are preserved.
func ReplaceFlowPortConfigs(existing []v1alpha1.TinyNodePortConfig, flowIDs map[string]bool, newConfigs []v1alpha1.TinyNodePortConfig) []v1alpha1.TinyNodePortConfig {
	clean := make([]v1alpha1.TinyNodePortConfig, 0, len(existing))
	for _, pc := range existing {
		if pc.From == "" || !flowIDs[pc.FlowID] {
			clean = append(clean, pc)
		}
	}
	return append(clean, newConfigs...)
}

// RemoveNodeReferences removes all edges targeting deletedNodeName and all
// port configs sourced from deletedNodeName. Mutates node.Spec in place.
// Returns true if anything was removed.
func RemoveNodeReferences(node *v1alpha1.TinyNode, deletedNodeName string) bool {
	changed := false

	cleanEdges := make([]v1alpha1.TinyNodeEdge, 0, len(node.Spec.Edges))
	for _, e := range node.Spec.Edges {
		targetNode, _ := ParseFullPortName(e.To)
		if targetNode == deletedNodeName {
			changed = true
			continue
		}
		cleanEdges = append(cleanEdges, e)
	}

	cleanPorts := make([]v1alpha1.TinyNodePortConfig, 0, len(node.Spec.Ports))
	for _, pc := range node.Spec.Ports {
		if pc.From != "" {
			fromNode, _ := ParseFullPortName(pc.From)
			if fromNode == deletedNodeName {
				changed = true
				continue
			}
		}
		cleanPorts = append(cleanPorts, pc)
	}

	if changed {
		node.Spec.Edges = cleanEdges
		node.Spec.Ports = cleanPorts
	}
	return changed
}
