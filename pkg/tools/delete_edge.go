package tools

import (
	"context"
	"fmt"
)

// DeleteEdgeTool removes an edge from the flow
type DeleteEdgeTool struct{}

func NewDeleteEdgeTool() *DeleteEdgeTool {
	return &DeleteEdgeTool{}
}

func (t *DeleteEdgeTool) Name() string {
	return "delete_edge"
}

func (t *DeleteEdgeTool) Description() string {
	return `Delete an edge (connection) from the current flow.

Removes the connection between two nodes without deleting the nodes themselves.
Use read_project first to get edge IDs.

Example:
  delete_edge(edge_id: "server-abc:response-router-def:input")
  → {deleted: true, edge_id: "server-abc:response-router-def:input"}`
}

func (t *DeleteEdgeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"edge_id": map[string]interface{}{
				"type":        "string",
				"description": "ID of the edge to delete (from read_project output)",
			},
		},
		"required": []string{"edge_id"},
	}
}

func (t *DeleteEdgeTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.FlowModifier == nil {
		return ToolResult{
			Success: false,
			Error:   "flow modifier not configured",
		}
	}

	edgeID, _ := input["edge_id"].(string)

	if edgeID == "" {
		return ToolResult{
			Success: false,
			Error:   "edge_id is required",
		}
	}

	// Delete edge using apply_changes mechanism
	operations := []FlowOperation{
		{
			Op: "delete",
			ID: edgeID,
			Element: map[string]interface{}{
				"type": "tinyEdge",
			},
		},
	}

	results, err := execCtx.FlowModifier.ApplyFlowChanges(ctx, execCtx.ProjectName, execCtx.FlowName, operations)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to delete edge: %s", err.Error()),
		}
	}

	if len(results) == 0 || !results[0].Success {
		errMsg := "unknown error"
		if len(results) > 0 && results[0].Error != "" {
			errMsg = results[0].Error
		}
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to delete edge: %s", errMsg),
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"deleted": true,
			"edge_id": edgeID,
		},
	}
}

var _ Tool = (*DeleteEdgeTool)(nil)
