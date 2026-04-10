package tools

import (
	"context"
	"fmt"
)

// AddEdgeTool connects two node ports
type AddEdgeTool struct{}

func NewAddEdgeTool() *AddEdgeTool {
	return &AddEdgeTool{}
}

func (t *AddEdgeTool) Name() string {
	return "add_edge"
}

func (t *AddEdgeTool) Description() string {
	return `Connect two node ports with an edge.

Specify the source node:port and target node:port.
Returns edge_id and whether configuration is needed.

If needs_configuration is true, use configure_edge to map data from source to target.
Before configuring, use get_node_port_schema on both ports to understand the data structures.

Example:
  add_edge(from_node: "server-abc", from_port: "request", to_node: "logger-def", to_port: "input")
  → {edge_id: "edge-xyz", needs_configuration: true}`
}

func (t *AddEdgeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"from_node": map[string]interface{}{
				"type":        "string",
				"description": "Source node ID",
			},
			"from_port": map[string]interface{}{
				"type":        "string",
				"description": "Source port name (e.g., 'request', 'output')",
			},
			"to_node": map[string]interface{}{
				"type":        "string",
				"description": "Target node ID",
			},
			"to_port": map[string]interface{}{
				"type":        "string",
				"description": "Target port name (e.g., 'input', 'response')",
			},
		},
		"required": []string{"from_node", "from_port", "to_node", "to_port"},
	}
}

func (t *AddEdgeTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.EdgeAdder == nil {
		return ToolResult{
			Success: false,
			Error:   "edge adder not configured",
		}
	}

	fromNode, _ := input["from_node"].(string)
	fromPort, _ := input["from_port"].(string)
	toNode, _ := input["to_node"].(string)
	toPort, _ := input["to_port"].(string)

	if fromNode == "" || fromPort == "" {
		return ToolResult{
			Success: false,
			Error:   "from_node and from_port are required",
		}
	}

	if toNode == "" || toPort == "" {
		return ToolResult{
			Success: false,
			Error:   "to_node and to_port are required",
		}
	}

	result, err := execCtx.EdgeAdder.AddEdge(ctx, execCtx.ProjectName, execCtx.FlowName, fromNode, fromPort, toNode, toPort)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to add edge: %s", err.Error()),
		}
	}

	output := map[string]interface{}{
		"edge_id":             result.EdgeID,
		"needs_configuration": result.NeedsConfiguration,
	}

	if result.NeedsConfiguration {
		output["hint"] = "Use configure_edge with configuration matching the target_port_schema below"

		// Fetch target port schema so model knows exactly what fields are required
		if execCtx.PortInspector != nil {
			targetPortInfo, err := execCtx.PortInspector.InspectPort(ctx, execCtx.ProjectName, toNode, toPort, "")
			if err == nil && targetPortInfo != nil {
				output["target_port_schema"] = targetPortInfo.Schema
				if targetPortInfo.ExampleData != nil {
					output["target_port_example"] = targetPortInfo.ExampleData
				}
			}
		}
	}

	return ToolResult{
		Success: true,
		Output:  output,
	}
}

var _ Tool = (*AddEdgeTool)(nil)
