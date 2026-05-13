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
	return `Get full details of a published solution: every flow, every node with its component/module/settings, every edge with its full configuration, plus any solution-level variables.

Returns the complete SolutionDetails payload so the caller can recreate the solution end-to-end. Pair with search_solutions to discover UUIDs.`
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

	// Wrap the full SolutionDetails — uuid, title, description, tags,
	// flows (with nested nodes/edges and configs), variables — in a
	// named field so the LLM can destructure consistently with other
	// tools that return wrapped maps.
	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"solution": solution,
			"hint":     "Use clone_solution(solution_uuid) to copy this solution's flows and nodes into the current project.",
		},
	}
}

var _ Tool = (*GetSolutionTool)(nil)
