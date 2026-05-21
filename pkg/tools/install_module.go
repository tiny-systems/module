package tools

import (
	"context"
	"fmt"
	"strings"
)

// InstallModuleTool installs a module into the active workspace
// cluster. Only registered in platform mode; the kubeconfig path
// has no built-in installer (users run `helm install` from the
// command surfaced by get_module_info).
//
// The tool blocks until the install reaches a terminal state.
// Progress is accumulated into a transcript that's returned with
// the final result — the LLM sees one synchronous response, the
// underlying gRPC stream and taskq job stay async server-side.
type InstallModuleTool struct{}

func NewInstallModuleTool() *InstallModuleTool {
	return &InstallModuleTool{}
}

func (t *InstallModuleTool) Name() string {
	return "install_module"
}

func (t *InstallModuleTool) Description() string {
	return `Install a Tiny Systems module into the workspace cluster. The module must already be in the public catalog (use search_modules or get_module_info to find it). Install is idempotent — calling on an already-installed module triggers a helm upgrade with the same values.

Bundles: modules can ship optional sub-charts (vector DBs, embedding endpoints, etc.). Omit the bundles field to install with the module's default selection. Pass an explicit list to enable only those bundles. Pass ["none"] as a sentinel to install with zero bundles (useful when you already run your own postgres/pgvector and don't need the bundled one).

Only available in platform mode. In kubeconfig mode, use the helm install command shown by get_module_info instead.`
}

func (t *InstallModuleTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"module_name": map[string]interface{}{
				"type":        "string",
				"description": "Module name, e.g. \"http-module-v0\" or \"tinysystems/http-module-v0\". Use search_modules first if unsure of the exact name.",
			},
			"version": map[string]interface{}{
				"type":        "string",
				"description": "Optional. Specific version to install (e.g. \"0.6.5\"). Omit to install the latest published version.",
			},
			"bundles": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional. Bundle aliases to enable. Omit for module defaults; [\"none\"] for zero bundles; otherwise a list of bundle names.",
			},
		},
		"required": []string{"module_name"},
	}
}

func (t *InstallModuleTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ModuleInstaller == nil {
		return ToolResult{
			Success: false,
			Error:   "install_module is not available in this mode. Use the helm install command from get_module_info instead, or re-run mcp-server with --platform-token to use platform mode.",
		}
	}

	moduleName, _ := input["module_name"].(string)
	if moduleName == "" {
		return ToolResult{Success: false, Error: "module_name is required"}
	}
	version, _ := input["version"].(string)
	bundles := parseBundles(input["bundles"])

	var transcript []string
	onProgress := func(p InstallProgress) {
		line := p.Message
		if p.LogType != "" && p.LogType != "info" {
			line = "[" + p.LogType + "] " + line
		}
		transcript = append(transcript, line)
	}

	result, err := execCtx.ModuleInstaller.InstallModule(ctx, moduleName, version, bundles, onProgress)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("install failed: %v", err),
			Output: map[string]interface{}{
				"transcript": strings.Join(transcript, "\n"),
			},
		}
	}
	if !result.Success {
		return ToolResult{
			Success: false,
			Error:   result.Error,
			Output: map[string]interface{}{
				"transcript": strings.Join(transcript, "\n"),
			},
		}
	}

	out := map[string]interface{}{
		"module_name": moduleName,
		"status":      "installed",
		"hint":        "Module is reconciling. Call list_modules in a few seconds to see its components, then build_flow or edit_flow to use them.",
	}
	if result.ModuleVersionID != "" {
		out["module_version_id"] = result.ModuleVersionID
	}
	if result.ReleaseName != "" {
		out["release_name"] = result.ReleaseName
	}
	if len(transcript) > 0 {
		out["transcript"] = strings.Join(transcript, "\n")
	}
	return ToolResult{Success: true, Output: out}
}

// parseBundles accepts either a JSON array of strings (from MCP
// client) or a single string. Returns nil for the absent / null
// case (caller wants module defaults); returns the slice otherwise.
// The ["none"] sentinel is passed through to the installer as a
// non-empty single-element slice — the installer recognises it.
func parseBundles(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	}
	return nil
}

var _ Tool = (*InstallModuleTool)(nil)
