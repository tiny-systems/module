package tools

import (
	"context"
)

// GetSolutionTool retrieves full details of a solution
type GetSolutionTool struct{}

func NewGetSolutionTool() *GetSolutionTool {
	return &GetSolutionTool{}
}

func (t *GetSolutionTool) Name() string {
	return "get_solution"
}

func (t *GetSolutionTool) Description() string {
	return `Get full details of a solution including all flows, nodes, edges, and configurations.

Returns:
- flows: All flows in the solution with their nodes and edges
- nodes: Node definitions with component, module, and settings
- edges: Edge connections with full configurations

Use this to:
- Learn how a solution is structured
- Copy patterns into the current project using apply_changes
- Understand what configurations make edges work`
}

func (t *GetSolutionTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"solution_uuid": map[string]interface{}{
				"type":        "string",
				"description": "Solution UUID from search_solutions",
			},
		},
		"required": []string{"solution_uuid"},
	}
}

func (t *GetSolutionTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.SolutionSearcher == nil {
		return ToolResult{
			Success: false,
			Error:   "solution search not configured",
		}
	}

	uuid, _ := input["solution_uuid"].(string)
	if uuid == "" {
		return ToolResult{
			Success: false,
			Error:   "solution_uuid is required",
		}
	}

	solution, err := execCtx.SolutionSearcher.GetSolution(ctx, uuid)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   "failed to get solution: " + err.Error(),
		}
	}

	if solution == nil {
		return ToolResult{
			Success: false,
			Error:   "solution not found",
		}
	}

	// Format flows with nodes and edges
	flows := make([]map[string]interface{}, len(solution.Flows))
	for i, flow := range solution.Flows {
		nodes := make([]map[string]interface{}, len(flow.Nodes))
		for j, node := range flow.Nodes {
			nodeMap := map[string]interface{}{
				"id":        node.ID,
				"component": node.Component,
				"module":    node.Module,
			}
			if node.Settings != nil {
				nodeMap["settings"] = node.Settings
			}
			if node.Position != nil {
				nodeMap["position"] = node.Position
			}
			nodes[j] = nodeMap
		}

		edges := make([]map[string]interface{}, len(flow.Edges))
		for j, edge := range flow.Edges {
			edgeMap := map[string]interface{}{
				"source":        edge.Source,
				"source_handle": edge.SourceHandle,
				"target":        edge.Target,
				"target_handle": edge.TargetHandle,
			}
			if edge.Configuration != nil {
				edgeMap["configuration"] = edge.Configuration
			}
			edges[j] = edgeMap
		}

		flows[i] = map[string]interface{}{
			"title": flow.Title,
			"nodes": nodes,
			"edges": edges,
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"uuid":        solution.UUID,
			"title":       solution.Title,
			"description": solution.Description,
			"tags":        solution.Tags,
			"flows":       flows,
			"hint":        "Use apply_changes to recreate these patterns in the current project",
		},
	}
}

var _ Tool = (*GetSolutionTool)(nil)
