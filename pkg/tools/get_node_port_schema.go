package tools

import (
	"context"
	"fmt"
)

// GetNodePortSchemaTool inspects a placed node's port to get actual schema and example data
// This uses recursive simulation through the flow graph, not static schema lookup
type GetNodePortSchemaTool struct{}

func NewGetNodePortSchemaTool() *GetNodePortSchemaTool {
	return &GetNodePortSchemaTool{}
}

func (t *GetNodePortSchemaTool) Name() string {
	return "get_node_port_schema"
}

func (t *GetNodePortSchemaTool) Description() string {
	return `Get the ACTUAL schema and example data for a placed node's port.

This is essential for edge configuration - you need to know:
1. What data is available from the SOURCE port
2. What data is expected by the TARGET port

IMPORTANT: This returns the ACTUAL configured schema, not default component schema.
Users can customize configurable ports to send different data types.

If trace_id is provided, returns REAL data from that execution instead of simulated examples.
This is useful for debugging actual production issues.

Always call this on BOTH source and target ports before configuring an edge.`
}

func (t *GetNodePortSchemaTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"node_id": map[string]interface{}{
				"type":        "string",
				"description": "The ID of the placed node",
			},
			"port": map[string]interface{}{
				"type":        "string",
				"description": "The port name (e.g., 'request', 'response', 'input', 'settings')",
			},
			"trace_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional trace ID to get real execution data instead of simulated examples",
			},
		},
		"required": []string{"node_id", "port"},
	}
}

func (t *GetNodePortSchemaTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.PortInspector == nil {
		return ToolResult{
			Success: false,
			Error:   "port inspector not configured",
		}
	}

	nodeID, _ := input["node_id"].(string)
	portName, _ := input["port"].(string)
	traceID, _ := input["trace_id"].(string)

	if nodeID == "" {
		return ToolResult{
			Success: false,
			Error:   "node_id is required",
		}
	}

	if portName == "" {
		return ToolResult{
			Success: false,
			Error:   "port is required",
		}
	}

	result, err := execCtx.PortInspector.InspectPort(ctx, execCtx.ProjectName, nodeID, portName, traceID)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to inspect port: %s", err.Error()),
		}
	}

	output := map[string]interface{}{
		"node_id":      result.NodeID,
		"port_name":    result.PortName,
		"port_type":    result.PortType,
		"schema":       result.Schema,
		"example_data": result.ExampleData,
	}

	if result.HasRealData {
		output["has_real_data"] = true
		output["note"] = "example_data contains REAL data from the specified trace"
	}

	if result.Configurable {
		output["configurable"] = true
		output["note"] = "This port's schema can be customized by the user"
	}

	if result.Description != "" {
		output["description"] = result.Description
	}

	return ToolResult{
		Success: true,
		Output:  output,
	}
}

var _ Tool = (*GetNodePortSchemaTool)(nil)
