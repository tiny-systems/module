package tools

import "context"

// SearchModulesTool discovers modules in the public Tiny Systems
// catalog — the counterpart to list_modules (which only sees modules
// already installed in the caller's cluster).
type SearchModulesTool struct{}

func NewSearchModulesTool() *SearchModulesTool {
	return &SearchModulesTool{}
}

func (t *SearchModulesTool) Name() string {
	return "search_modules"
}

func (t *SearchModulesTool) Description() string {
	return `Search the public Tiny Systems module catalog for modules available to install.

Use this when:
- list_modules returns empty (no modules installed in this cluster yet)
- A solution you want to clone references a module that is not in list_modules' output
- The user asks about a capability and you are not sure which module provides it

Returns summaries (name, description, latest version, facet flags). Call get_module_info with the chosen module name to see components, port schemas, RBAC permissions, and helm install instructions.

The distinction that matters:
- list_modules = what is running in the cluster right now
- search_modules = what is available to install from the catalog
Both are valid, and you may need to use search_modules + get_module_info to tell the user how to install a module before you can use list_modules / get_component_info on it.`
}

func (t *SearchModulesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Natural-language query matched against module name and description (e.g. 'slack messaging', 'kubernetes pods', 'http server').",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return (default 20, max 100).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchModulesTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.PublicModuleCatalog == nil {
		return ToolResult{
			Success: false,
			Error:   "public module catalog not configured",
		}
	}

	query, _ := input["query"].(string)
	if query == "" {
		return ToolResult{
			Success: false,
			Error:   "query is required",
		}
	}

	limit := 20
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	if l, ok := input["limit"].(int); ok && l > 0 {
		limit = l
	}

	results, err := execCtx.PublicModuleCatalog.SearchModules(ctx, query, limit)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   "search modules failed: " + err.Error(),
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"count":   len(results),
			"modules": results,
			"hint":    "Use get_module_info with a module name to see components, port schemas, and install instructions.",
		},
	}
}

var _ Tool = (*SearchModulesTool)(nil)
