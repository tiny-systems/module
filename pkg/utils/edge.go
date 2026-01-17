package utils

import (
	"github.com/google/uuid"
	"github.com/tiny-systems/module/api/v1alpha1"
)

// NewEdgeID generates a new unique edge ID using UUID.
func NewEdgeID() string {
	return uuid.New().String()
}

// FormatEdgeTarget formats the target endpoint for an edge in the format "node:port".
// This is used for the TinyNodeEdge.To field.
func FormatEdgeTarget(targetNode, targetPort string) string {
	return GetPortFullName(targetNode, targetPort)
}

// ParseEdgeTarget parses an edge target string into node and port components.
// This is used to parse the TinyNodeEdge.To field.
func ParseEdgeTarget(target string) (node string, port string) {
	return ParseFullPortName(target)
}

// NewEdge creates a new TinyNodeEdge with proper formatting.
// This ensures consistent edge creation across platform and desktop-client.
func NewEdge(sourcePort, targetNode, targetPort, flowID string) v1alpha1.TinyNodeEdge {
	return v1alpha1.TinyNodeEdge{
		ID:     NewEdgeID(),
		Port:   sourcePort,
		To:     FormatEdgeTarget(targetNode, targetPort),
		FlowID: flowID,
	}
}

// NewEdgeWithID creates a new TinyNodeEdge with a specific ID.
// Use this when you need to preserve an existing edge ID.
func NewEdgeWithID(id, sourcePort, targetNode, targetPort, flowID string) v1alpha1.TinyNodeEdge {
	return v1alpha1.TinyNodeEdge{
		ID:     id,
		Port:   sourcePort,
		To:     FormatEdgeTarget(targetNode, targetPort),
		FlowID: flowID,
	}
}

// NewPortConfig creates a new TinyNodePortConfig for an edge connection.
// The "From" field uses the format "targetNode:targetPort" to indicate where data comes from.
func NewPortConfig(sourcePort, targetNode, targetPort, flowID string, configuration []byte) v1alpha1.TinyNodePortConfig {
	return v1alpha1.TinyNodePortConfig{
		Port:          sourcePort,
		From:          FormatEdgeTarget(targetNode, targetPort),
		Configuration: configuration,
		FlowID:        flowID,
	}
}

// NewPortConfigWithSchema creates a new TinyNodePortConfig with schema information.
func NewPortConfigWithSchema(sourcePort, targetNode, targetPort, flowID string, configuration, schema []byte) v1alpha1.TinyNodePortConfig {
	return v1alpha1.TinyNodePortConfig{
		Port:          sourcePort,
		From:          FormatEdgeTarget(targetNode, targetPort),
		Configuration: configuration,
		Schema:        schema,
		FlowID:        flowID,
	}
}

// AddEdgeToNode adds an edge to a node's edge list.
// Returns the created edge.
func AddEdgeToNode(node *v1alpha1.TinyNode, sourcePort, targetNode, targetPort, flowID string) v1alpha1.TinyNodeEdge {
	edge := NewEdge(sourcePort, targetNode, targetPort, flowID)
	node.Spec.Edges = append(node.Spec.Edges, edge)
	return edge
}

// RemoveEdgeFromNode removes an edge by ID from a node's edge list.
// Returns the removed edge's "To" field if found, or empty string if not found.
func RemoveEdgeFromNode(node *v1alpha1.TinyNode, edgeID string) string {
	var targetTo string
	newEdges := make([]v1alpha1.TinyNodeEdge, 0, len(node.Spec.Edges))
	for _, edge := range node.Spec.Edges {
		if edge.ID == edgeID {
			targetTo = edge.To
			continue
		}
		newEdges = append(newEdges, edge)
	}
	node.Spec.Edges = newEdges
	return targetTo
}

// RemovePortConfigByFrom removes port configurations that match the given "From" field.
// This is typically called after removing an edge to clean up associated port configs.
func RemovePortConfigByFrom(node *v1alpha1.TinyNode, from string) {
	if from == "" {
		return
	}
	newPorts := make([]v1alpha1.TinyNodePortConfig, 0, len(node.Spec.Ports))
	for _, portConfig := range node.Spec.Ports {
		if portConfig.From == from {
			continue
		}
		newPorts = append(newPorts, portConfig)
	}
	node.Spec.Ports = newPorts
}

// DisconnectEdge removes an edge and its associated port configurations from a node.
// This is a convenience function that combines RemoveEdgeFromNode and RemovePortConfigByFrom.
func DisconnectEdge(node *v1alpha1.TinyNode, edgeID string) {
	targetTo := RemoveEdgeFromNode(node, edgeID)
	RemovePortConfigByFrom(node, targetTo)
}
