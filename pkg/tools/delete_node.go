package tools

import (
	"context"
	"fmt"
)

// DeleteNodeTool removes a node from the flow
type DeleteNodeTool struct{}

func NewDeleteNodeTool() *DeleteNodeTool {
	return &DeleteNodeTool{}
}

func (t *DeleteNodeTool) Name() string {
	return "delete_node"
}

func (t *DeleteNodeTool) Description() string {
	return `Delete a node from the current flow.

Removes the specified node and all edges connected to it.
Use read_project first to get node IDs.

Example:
  delete_node(node_id: "router-abc123")
  → {deleted: true, node_id: "router-abc123"}`
}

func (t *DeleteNodeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"node_id": map[string]interface{}{
				"type":        "string",
				"description": "ID of the node to delete (from read_project output)",
			},
		},
		"required": []string{"node_id"},
	}
}

func (t *DeleteNodeTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.FlowModifier == nil {
		return ToolResult{
			Success: false,
			Error:   "flow modifier not configured",
		}
	}

	nodeID, _ := input["node_id"].(string)

	if nodeID == "" {
		return ToolResult{
			Success: false,
			Error:   "node_id is required",
		}
	}

	// Delete node using apply_changes mechanism
	operations := []FlowOperation{
		{
			Op: "delete",
			ID: nodeID,
			Element: map[string]interface{}{
				"type": "tinyNode",
			},
		},
	}

	results, err := execCtx.FlowModifier.ApplyFlowChanges(ctx, execCtx.ProjectName, execCtx.FlowName, operations)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to delete node: %s", err.Error()),
		}
	}

	if len(results) == 0 || !results[0].Success {
		errMsg := "unknown error"
		if len(results) > 0 && results[0].Error != "" {
			errMsg = results[0].Error
		}
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to delete node: %s", errMsg),
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"deleted": true,
			"node_id": nodeID,
		},
	}
}

var _ Tool = (*DeleteNodeTool)(nil)
