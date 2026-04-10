package tools

import (
	"context"
	"fmt"
)

// CreateFlowTool creates a new flow in the current project
type CreateFlowTool struct{}

func NewCreateFlowTool() *CreateFlowTool {
	return &CreateFlowTool{}
}

func (t *CreateFlowTool) Name() string {
	return "create_flow"
}

func (t *CreateFlowTool) Description() string {
	return `Create a new empty flow in the current project. Returns the flow's resource name which can be used with build_flow and other tools.`
}

func (t *CreateFlowTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Human-readable name for the flow",
			},
		},
		"required": []string{"name"},
	}
}

func (t *CreateFlowTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.FlowCreator == nil {
		return ToolResult{Success: false, Error: "flow creator not configured"}
	}

	name, _ := input["name"].(string)
	if name == "" {
		return ToolResult{Success: false, Error: "name is required"}
	}

	resourceName, err := execCtx.FlowCreator.CreateFlow(ctx, execCtx.ProjectName, name)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to create flow: %v", err)}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"flow_name":     resourceName,
			"display_name":  name,
			"status":        "flow created",
		},
	}
}

var _ Tool = (*CreateFlowTool)(nil)
