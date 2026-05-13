package tools

import (
	"context"
	"fmt"
)

// DeleteFlowTool deletes a flow from the current project
type DeleteFlowTool struct{}

func NewDeleteFlowTool() *DeleteFlowTool {
	return &DeleteFlowTool{}
}

func (t *DeleteFlowTool) Name() string {
	return "delete_flow"
}

func (t *DeleteFlowTool) Description() string {
	return `Delete a flow and all its nodes/edges from the current project. This is irreversible. Use read_project first to confirm the flow resource name.`
}

func (t *DeleteFlowTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"flow": map[string]interface{}{
				"type":        "string",
				"description": "Flow resource name to delete",
			},
		},
		"required": []string{"flow"},
	}
}

func (t *DeleteFlowTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.FlowDeleter == nil {
		return ToolResult{Success: false, Error: "flow deleter not configured"}
	}

	flowName, _ := input["flow"].(string)
	if flowName == "" {
		return ToolResult{Success: false, Error: "flow is required. Use read_project to list flows in the current project."}
	}

	if err := execCtx.FlowDeleter.DeleteFlow(ctx, execCtx.ProjectName, flowName); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to delete flow: %v", err)}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"deleted":   true,
			"flow_name": flowName,
		},
	}
}

var _ Tool = (*DeleteFlowTool)(nil)
