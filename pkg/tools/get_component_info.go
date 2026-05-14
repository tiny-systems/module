package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetComponentInfoTool gets detailed info about a specific component
// via the injected ModuleCatalog.
type GetComponentInfoTool struct{}

func NewGetComponentInfoTool() *GetComponentInfoTool {
	return &GetComponentInfoTool{}
}

func (t *GetComponentInfoTool) Name() string {
	return "get_component_info"
}

func (t *GetComponentInfoTool) Description() string {
	return `Get detailed information about a component including description, port schemas, and example data.

Returns the full schema for each port so you know exactly what fields to map in edge configurations.
Always call this BEFORE using build_flow to understand what fields each port expects.

Example: get_component_info(module_name: "tinysystems/common-module", component: "ticker")`
}

func (t *GetComponentInfoTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"module_name": map[string]interface{}{
				"type":        "string",
				"description": "The module name (e.g., 'tinysystems/common-module' or 'common-module')",
			},
			"component": map[string]interface{}{
				"type":        "string",
				"description": "The component name within the module (e.g., 'ticker', 'router', 'signal')",
			},
		},
		"required": []string{"component"},
	}
}

func (t *GetComponentInfoTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ModuleCatalog == nil {
		return ToolResult{
			Success: false,
			Error:   "module catalog not configured",
		}
	}

	moduleName, _ := input["module_name"].(string)
	componentName, _ := input["component"].(string)

	if componentName == "" {
		return ToolResult{
			Success: false,
			Error:   "component is required",
		}
	}

	// If moduleName is provided, look up that specific module.
	// Otherwise, list all modules and search for the component by name.
	var modules []ModuleInfo
	if moduleName != "" {
		m, err := execCtx.ModuleCatalog.GetModule(ctx, moduleName)
		if err != nil {
			return ToolResult{
				Success: false,
				Error:   "failed to get module: " + err.Error(),
			}
		}
		if m == nil {
			return ToolResult{
				Success: false,
				Error:   fmt.Sprintf("module '%s' not found", moduleName),
			}
		}
		modules = []ModuleInfo{*m}
	} else {
		all, err := execCtx.ModuleCatalog.ListModules(ctx)
		if err != nil {
			return ToolResult{
				Success: false,
				Error:   "failed to list modules: " + err.Error(),
			}
		}
		// For each module in the list, fetch full details so we get schemas
		modules = make([]ModuleInfo, 0, len(all))
		for _, m := range all {
			full, err := execCtx.ModuleCatalog.GetModule(ctx, m.Name)
			if err != nil || full == nil {
				// Fall back to the light ListModules data
				modules = append(modules, m)
				continue
			}
			modules = append(modules, *full)
		}
	}

	for _, m := range modules {
		for _, c := range m.Components {
			if c.Name != componentName {
				continue
			}

			// Build detailed response with port schemas
			compInfo := map[string]interface{}{
				"module_name": m.Name,
				"name":        c.Name,
				"description": c.Description,
			}
			if c.Info != "" {
				compInfo["info"] = c.Info
			}
			if m.Version != "" {
				compInfo["module_version"] = m.Version
			}

			// Assemble port lists with schema detail (from InputPortDetails / OutputPortDetails)
			// Falls back to plain names if details aren't populated.
			inputPorts := buildPortList(c.InputPorts, c.InputPortDetails)
			outputPorts := buildPortList(c.OutputPorts, c.OutputPortDetails)

			if len(inputPorts) > 0 {
				compInfo["input_ports"] = inputPorts
			}
			if len(outputPorts) > 0 {
				compInfo["output_ports"] = outputPorts
			}
			compInfo["has_output"] = len(outputPorts) > 0

			return ToolResult{
				Success: true,
				Output:  compInfo,
			}
		}
	}

	// Not found
	errMsg := fmt.Sprintf("component '%s' not found", componentName)
	if moduleName != "" {
		errMsg = fmt.Sprintf("component '%s' not found in module '%s'", componentName, moduleName)
	}
	return ToolResult{
		Success: false,
		Error:   errMsg,
	}
}

// buildPortList merges a name list with a details list.
// Detailed entries (with schema/example) override plain names.
func buildPortList(names []string, details []PortDetail) []map[string]interface{} {
	// Build a map of name → detail for quick lookup
	detailMap := make(map[string]PortDetail, len(details))
	for _, d := range details {
		detailMap[d.Name] = d
	}

	// Use details as source of truth if available, otherwise fall back to names
	source := names
	if len(details) > 0 {
		source = make([]string, 0, len(details))
		for _, d := range details {
			source = append(source, d.Name)
		}
	}

	result := make([]map[string]interface{}, 0, len(source))
	seen := make(map[string]bool, len(source))
	for _, name := range source {
		// Skip system ports
		if name == "_settings" || name == "_control" || name == "_reconcile" || name == "_client" || name == "_identity" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true

		portInfo := map[string]interface{}{"name": name}
		if d, ok := detailMap[name]; ok {
			if d.Description != "" {
				portInfo["description"] = d.Description
			}
			if len(d.Schema) > 0 {
				var schema interface{}
				if err := json.Unmarshal(d.Schema, &schema); err == nil {
					portInfo["schema"] = schema
				}
			}
			if len(d.Example) > 0 {
				var example interface{}
				if err := json.Unmarshal(d.Example, &example); err == nil {
					portInfo["example"] = example
				}
			}
		}
		result = append(result, portInfo)
	}
	return result
}

var _ Tool = (*GetComponentInfoTool)(nil)
