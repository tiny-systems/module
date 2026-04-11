package tools

import "context"

// GetModuleInfoTool fetches full module details from the public Tiny
// Systems catalog — components with port schemas, RBAC permissions,
// and helm install instructions. Companion to search_modules.
type GetModuleInfoTool struct{}

func NewGetModuleInfoTool() *GetModuleInfoTool {
	return &GetModuleInfoTool{}
}

func (t *GetModuleInfoTool) Name() string {
	return "get_module_info"
}

func (t *GetModuleInfoTool) Description() string {
	return `Get full details about a module in the public Tiny Systems catalog: description, latest version, every component with its port schemas, RBAC permissions, and helm install instructions.

Call this after search_modules when you have a module name and need:
- The list of components the module ships and their typed port schemas
- Whether the module needs Kubernetes API access or persistent storage
- The exact helm command + fields the user must run to install the module locally

This is the catalog equivalent of get_component_info — but get_component_info reads from modules already installed in the cluster, while get_module_info reads from the published catalog and works even when no module is installed.

When the LLM decides the user needs to install a module, quote the returned helm_install.command as-is (it is a template with placeholders) and list the prerequisites and warnings verbatim so the user understands what they are agreeing to.`
}

func (t *GetModuleInfoTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"module": map[string]interface{}{
				"type":        "string",
				"description": "Module name. Accepts base name (e.g. 'common-module-v0') or workspace-qualified name (e.g. 'tinysystems/common-module-v0').",
			},
		},
		"required": []string{"module"},
	}
}

func (t *GetModuleInfoTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.PublicModuleCatalog == nil {
		return ToolResult{
			Success: false,
			Error:   "public module catalog not configured",
		}
	}

	name, _ := input["module"].(string)
	if name == "" {
		return ToolResult{
			Success: false,
			Error:   "module is required",
		}
	}

	details, err := execCtx.PublicModuleCatalog.GetPublicModule(ctx, name)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   "get module info failed: " + err.Error(),
		}
	}
	if details == nil {
		return ToolResult{
			Success: false,
			Error:   "module '" + name + "' not found in public catalog",
		}
	}

	return ToolResult{
		Success: true,
		Output:  details,
	}
}

var _ Tool = (*GetModuleInfoTool)(nil)
