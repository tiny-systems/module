package tools

import (
	"context"
	"fmt"
)

// ReadProjectTool retrieves all project elements with flow ownership information
type ReadProjectTool struct{}

func NewReadProjectTool() *ReadProjectTool {
	return &ReadProjectTool{}
}

func (t *ReadProjectTool) Name() string {
	return "read_project"
}

func (t *ReadProjectTool) Description() string {
	return `Get the complete project structure including all flows and their elements. THIS IS READ-ONLY - use apply_changes to make modifications.

Returns:
- flows: List of all flows in the project with their resource_name and title
- elements: All nodes and edges with flow ownership information

Each element includes:
- flow: The flow this element belongs to (resource_name)
- flow_title: Human-readable flow title
- shared_with: (nodes only) List of other flows that connect TO this node

IMPORTANT: This tool only READS the current state. To add nodes, edges, or make any changes, you MUST use the apply_changes tool.`
}

func (t *ReadProjectTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}
}

func (t *ReadProjectTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ProjectName == "" {
		return ToolResult{
			Success: false,
			Error:   "project context is required",
		}
	}

	if execCtx.ProjectReader == nil {
		return ToolResult{
			Success: false,
			Error:   "project reader not configured",
		}
	}

	// Fetch all project elements
	projectData, err := execCtx.ProjectReader.ReadProjectElements(ctx, execCtx.ProjectName)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to read project: %v", err),
		}
	}

	// Count elements by type and flow
	nodeCount := 0
	edgeCount := 0
	flowNodeCounts := make(map[string]int)
	sharedNodeCount := 0

	for _, elem := range projectData.Elements {
		elemType, _ := elem["type"].(string)
		flowName, _ := elem["flow"].(string)

		if elemType == "tinyNode" {
			nodeCount++
			flowNodeCounts[flowName]++
			if _, hasShared := elem["shared_with"]; hasShared {
				sharedNodeCount++
			}
		} else if elemType == "tinyEdge" {
			edgeCount++
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"project":            execCtx.ProjectName,
			"current_flow":       execCtx.FlowName,
			"current_flow_title": execCtx.FlowTitle,
			"flows":              projectData.Flows,
			"summary": map[string]interface{}{
				"total_flows":    len(projectData.Flows),
				"total_nodes":    nodeCount,
				"total_edges":    edgeCount,
				"shared_nodes":   sharedNodeCount,
				"nodes_per_flow": flowNodeCounts,
			},
			"elements": projectData.Elements,
		},
	}
}

var _ Tool = (*ReadProjectTool)(nil)
