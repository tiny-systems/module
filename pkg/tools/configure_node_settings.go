package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ConfigureNodeSettingsTool sets the settings for a node's _settings port
type ConfigureNodeSettingsTool struct{}

func NewConfigureNodeSettingsTool() *ConfigureNodeSettingsTool {
	return &ConfigureNodeSettingsTool{}
}

func (t *ConfigureNodeSettingsTool) Name() string {
	return "configure_node_settings"
}

func (t *ConfigureNodeSettingsTool) Description() string {
	return `Configure a node's settings (the _settings port).

Use this to set component-specific configuration like:
- Router: routes=["GET", "POST"] to define routing options
- Ticker: delay interval and context
- Other component-specific settings

IMPORTANT: Before configuring, use get_node_port_schema on the node's "_settings" port
to understand what settings are available and their expected types.

## Extending Configurable Schemas - CRITICAL for Ticker/Signal

When a settings field has configurable:true (like Ticker's context), you MUST provide a "schema"
parameter along with settings. Think of schema as DEFINING YOUR TYPE - like declaring a struct in Go
or an interface in TypeScript. Without schema:
- Context is just "any" (empty {}) - the system doesn't know what fields exist
- Edge validation fails because it can't verify {{$.slackToken}} refers to a real field
- You'll get "got null" or "field not found" errors

The schema parameter defines TYPE STRUCTURE for configurable fields, which propagates to output
ports using the same definition.

Example - Ticker with user config (ALWAYS include schema):
  configure_node_settings(node_id: "ticker-xyz", settings: {
    "delay": 30000,
    "context": {
      "slackToken": "xoxb-placeholder",
      "namespace": "default"
    }
  }, schema: {
    "context": {
      "type": "object",
      "properties": {
        "slackToken": {"type": "string", "title": "Slack Token"},
        "namespace": {"type": "string", "title": "Namespace"}
      },
      "required": ["slackToken", "namespace"]
    }
  })

After this, Ticker's "out" port schema includes slackToken and namespace fields, so downstream
edge expressions like {{$.slackToken}} validate correctly.`
}

func (t *ConfigureNodeSettingsTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"node_id": map[string]interface{}{
				"type":        "string",
				"description": "The node ID to configure settings for",
			},
			"settings": map[string]interface{}{
				"description": "The settings configuration matching the _settings port schema. Can be a JSON object or a JSON string that parses to an object.",
			},
			"schema": map[string]interface{}{
				"type":        "object",
				"description": "Optional schema extensions for configurable fields. Keys are field names (e.g., 'context'), values are JSON Schema definitions that extend/override the field's schema.",
			},
		},
		"required": []string{"node_id", "settings"},
	}
}

func (t *ConfigureNodeSettingsTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.NodeSettingsConfigurer == nil {
		return ToolResult{
			Success: false,
			Error:   "node settings configurer not configured",
		}
	}

	nodeID, _ := input["node_id"].(string)
	schema, _ := input["schema"].(map[string]interface{})

	if nodeID == "" {
		return ToolResult{
			Success: false,
			Error:   "node_id is required",
		}
	}

	settings, settingsOk := input["settings"].(map[string]interface{})
	if !settingsOk {
		if settingsStr, isString := input["settings"].(string); isString {
			if err := json.Unmarshal([]byte(settingsStr), &settings); err != nil {
				return ToolResult{
					Success: false,
					Error:   fmt.Sprintf("settings string is not valid JSON: %v", err),
				}
			}
		} else {
			return ToolResult{
				Success: false,
				Error:   "settings is required and must be a JSON object or a JSON string",
			}
		}
	}

	result, err := execCtx.NodeSettingsConfigurer.ConfigureNodeSettings(ctx, execCtx.ProjectName, execCtx.FlowName, nodeID, settings, schema)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to configure node settings: %s", err.Error()),
		}
	}

	if !result.Valid {
		output := map[string]interface{}{
			"valid": false,
			"error": result.Error,
		}
		if result.Hint != "" {
			output["hint"] = result.Hint
		}
		return ToolResult{
			Success: false,
			Error:   result.Error,
			Output:  output,
		}
	}

	output := map[string]interface{}{
		"valid":   true,
		"node_id": nodeID,
	}
	// Include updated ports if settings changed them (e.g., router routes create new output ports)
	if len(result.Ports) > 0 {
		output["ports"] = result.Ports
		output["hint"] = "Settings may have changed available ports. Use these port names for add_edge."
	}

	return ToolResult{
		Success: true,
		Output:  output,
	}
}

var _ Tool = (*ConfigureNodeSettingsTool)(nil)
