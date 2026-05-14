package tools

import (
	"context"
)

// ListModulesTool lists available modules via the injected ModuleCatalog.
// Backend is pluggable: hosted platform reads from DB, public MCP reads
// TinyModule CRDs from the current namespace.
type ListModulesTool struct{}

func NewListModulesTool() *ListModulesTool {
	return &ListModulesTool{}
}

func (t *ListModulesTool) Name() string {
	return "list_modules"
}

func (t *ListModulesTool) Description() string {
	return "List all available modules. Each module contains components that can be added to flows."
}

func (t *ListModulesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}
}

func (t *ListModulesTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ModuleCatalog == nil {
		return ToolResult{
			Success: false,
			Error:   "module catalog not configured",
		}
	}

	modules, err := execCtx.ModuleCatalog.ListModules(ctx)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   "failed to list modules: " + err.Error(),
		}
	}

	result := make([]map[string]interface{}, 0, len(modules))
	for _, m := range modules {
		moduleInfo := map[string]interface{}{
			"name":        m.Name,
			"description": m.Description,
		}
		if m.Version != "" {
			moduleInfo["version"] = m.Version
		}

		components := make([]map[string]interface{}, 0, len(m.Components))
		for _, c := range m.Components {
			compInfo := map[string]interface{}{
				"name":        c.Name,
				"description": c.Description,
			}
			if c.Info != "" {
				compInfo["info"] = c.Info
			}
			if len(c.InputPorts) > 0 {
				compInfo["input_ports"] = c.InputPorts
			}
			if len(c.OutputPorts) > 0 {
				compInfo["output_ports"] = c.OutputPorts
			}
			// Mark as sink if no output ports.
			// Critical for LLM to know which components can emit data.
			compInfo["has_output"] = len(c.OutputPorts) > 0
			components = append(components, compInfo)
		}
		moduleInfo["components"] = components
		result = append(result, moduleInfo)
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"modules": result,
			"total":   len(result),
			"hint":    "Use get_component_info(component, module) for detailed port schemas and behavior notes before wiring a component into a flow.",
		},
	}
}

var _ Tool = (*ListModulesTool)(nil)
