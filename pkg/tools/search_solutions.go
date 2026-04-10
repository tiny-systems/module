package tools

import (
	"context"
)

// SearchSolutionsTool searches for solutions by keyword and tags
type SearchSolutionsTool struct{}

func NewSearchSolutionsTool() *SearchSolutionsTool {
	return &SearchSolutionsTool{}
}

func (t *SearchSolutionsTool) Name() string {
	return "search_solutions"
}

func (t *SearchSolutionsTool) Description() string {
	return `Search for existing solutions that demonstrate integration patterns.

Returns solution summaries with UUIDs. Use get_solution for full details.

Use this when:
- User asks how to build a common integration
- You need examples of how to configure specific components
- User wants to see existing patterns before you build

Note: To install a solution, users check it out as a new project from the marketplace.`
}

func (t *SearchSolutionsTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Natural language description of what you're looking for",
			},
			"tags": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional tags to filter by (e.g., 'webhook', 'email', 'http')",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchSolutionsTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.SolutionSearcher == nil {
		return ToolResult{
			Success: false,
			Error:   "solution search not configured",
		}
	}

	query, _ := input["query"].(string)
	if query == "" {
		return ToolResult{
			Success: false,
			Error:   "query is required",
		}
	}

	// Extract tags if provided
	var tags []string
	if tagsRaw, ok := input["tags"].([]interface{}); ok {
		for _, tagRaw := range tagsRaw {
			if tag, ok := tagRaw.(string); ok {
				tags = append(tags, tag)
			}
		}
	}

	// Search with reasonable limit
	solutions, err := execCtx.SolutionSearcher.SearchSolutions(ctx, query, tags, 10)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   "failed to search solutions: " + err.Error(),
		}
	}

	// Format as summaries
	summaries := make([]map[string]interface{}, len(solutions))
	for i, sol := range solutions {
		summaries[i] = map[string]interface{}{
			"uuid":        sol.UUID,
			"title":       sol.Title,
			"description": sol.Description,
			"tags":        sol.Tags,
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"solutions": summaries,
			"count":     len(solutions),
			"hint":      "Use get_solution with a UUID to see full structure with nodes and edges",
		},
	}
}

var _ Tool = (*SearchSolutionsTool)(nil)
