package tools

import (
	"context"
	"fmt"
)

// UninstallModuleTool removes a module from the active workspace
// cluster. Only registered in platform mode. The platform refuses
// the call if any TinyNode still references the module — the LLM
// should delete those flows first.
type UninstallModuleTool struct{}

func NewUninstallModuleTool() *UninstallModuleTool {
	return &UninstallModuleTool{}
}

func (t *UninstallModuleTool) Name() string {
	return "uninstall_module"
}

func (t *UninstallModuleTool) Description() string {
	return `Remove a module's deployment from the workspace cluster. Refuses when any node in any flow still references the module — delete those flows (or call edit_flow to remove the offending nodes) first, then retry. The response's in_use field lists the blocking nodes.

Only available in platform mode. In kubeconfig mode, run "helm uninstall <release>" against the cluster yourself.`
}

func (t *UninstallModuleTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"module_name": map[string]interface{}{
				"type":        "string",
				"description": "Module name, e.g. \"http-module-v0\" or \"tinysystems/http-module-v0\".",
			},
		},
		"required": []string{"module_name"},
	}
}

func (t *UninstallModuleTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ModuleUninstaller == nil {
		return ToolResult{
			Success: false,
			Error:   "uninstall_module is not available in this mode. Run \"helm uninstall <release>\" directly, or re-run mcp-server with --platform-token to use platform mode.",
		}
	}

	moduleName, _ := input["module_name"].(string)
	if moduleName == "" {
		return ToolResult{Success: false, Error: "module_name is required"}
	}

	result, err := execCtx.ModuleUninstaller.UninstallModule(ctx, moduleName)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("uninstall failed: %v", err)}
	}
	if !result.Success {
		out := map[string]interface{}{}
		if len(result.InUse) > 0 {
			out["in_use"] = result.InUse
			out["hint"] = "Delete the flows that contain these nodes, or use edit_flow to remove them, then retry uninstall_module."
		}
		return ToolResult{Success: false, Error: result.Error, Output: out}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"module_name": moduleName,
			"status":      "uninstalled",
		},
	}
}

var _ Tool = (*UninstallModuleTool)(nil)
