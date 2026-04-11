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

	// Return the full SolutionDetails struct directly. JSON tags on the
	// struct handle serialization, so every field — uuid, title,
	// description, tags, flows (with nested nodes/edges and their full
	// configurations), and variables — reaches the caller without any
	// manual reshaping that could drop fields.
	return ToolResult{
		Success: true,
		Output:  solution,
	}
}

var _ Tool = (*GetSolutionTool)(nil)
