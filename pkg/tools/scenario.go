package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// CreateScenarioTool creates a scenario from a trace
type CreateScenarioTool struct{}

func NewCreateScenarioTool() *CreateScenarioTool {
	return &CreateScenarioTool{}
}

func (t *CreateScenarioTool) Name() string {
	return "create_scenario"
}

func (t *CreateScenarioTool) Description() string {
	return `Create a scenario for compile-time edge validation. Two modes:

1. From trace (recommended when available): pass trace_id to capture real port data from a previous execution.
2. Empty: omit trace_id to create a blank scenario, then populate ports with update_scenario.

Examples:
  create_scenario(name: "Happy path", trace_id: "abc123")  — from trace
  create_scenario(name: "user signup payload")               — empty, populate with update_scenario`
}

func (t *CreateScenarioTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Human-readable name for the scenario",
			},
			"trace_id": map[string]interface{}{
				"type":        "string",
				"description": "Trace ID to capture data from (returned by send_signal)",
			},
		},
		"required": []string{"name"},
	}
}

func (t *CreateScenarioTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ScenarioManager == nil {
		return ToolResult{Success: false, Error: "scenario manager not configured"}
	}

	name, _ := input["name"].(string)
	if name == "" {
		return ToolResult{Success: false, Error: "name is required"}
	}

	traceID, _ := input["trace_id"].(string)

	var item *ScenarioItem
	var err error
	if traceID != "" {
		item, err = execCtx.ScenarioManager.CreateScenarioFromTrace(ctx, execCtx.ProjectName, name, traceID)
	} else {
		item, err = execCtx.ScenarioManager.CreateEmptyScenario(ctx, execCtx.ProjectName, name)
	}
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to create scenario: %v", err)}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"resource_name": item.ResourceName,
			"name":          item.Name,
			"port_count":    item.PortCount,
		},
	}
}

var _ Tool = (*CreateScenarioTool)(nil)

// DeleteScenarioTool deletes a scenario
type DeleteScenarioTool struct{}

func NewDeleteScenarioTool() *DeleteScenarioTool {
	return &DeleteScenarioTool{}
}

func (t *DeleteScenarioTool) Name() string {
	return "delete_scenario"
}

func (t *DeleteScenarioTool) Description() string {
	return `Delete a scenario by its resource name. Use list_scenarios to find available scenarios first.`
}

func (t *DeleteScenarioTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"resource_name": map[string]interface{}{
				"type":        "string",
				"description": "Scenario resource name (from list_scenarios)",
			},
		},
		"required": []string{"resource_name"},
	}
}

func (t *DeleteScenarioTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ScenarioManager == nil {
		return ToolResult{Success: false, Error: "scenario manager not configured"}
	}

	resourceName, _ := input["resource_name"].(string)
	if resourceName == "" {
		return ToolResult{Success: false, Error: "resource_name is required"}
	}

	if err := execCtx.ScenarioManager.DeleteScenario(ctx, execCtx.ProjectName, resourceName); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to delete scenario: %v", err)}
	}

	return ToolResult{
		Success: true,
		Output:  map[string]interface{}{"deleted": resourceName},
	}
}

var _ Tool = (*DeleteScenarioTool)(nil)

// ListScenariosTool lists scenarios for the current project
type ListScenariosTool struct{}

func NewListScenariosTool() *ListScenariosTool {
	return &ListScenariosTool{}
}

func (t *ListScenariosTool) Name() string {
	return "list_scenarios"
}

func (t *ListScenariosTool) Description() string {
	return `List all scenarios for the current project. Scenarios contain sample port data for compile-time edge validation.`
}

func (t *ListScenariosTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ListScenariosTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ScenarioManager == nil {
		return ToolResult{Success: false, Error: "scenario manager not configured"}
	}

	items, err := execCtx.ScenarioManager.ListScenarios(ctx, execCtx.ProjectName)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to list scenarios: %v", err)}
	}

	return ToolResult{
		Success: true,
		Output:  items,
	}
}

var _ Tool = (*ListScenariosTool)(nil)

// UpdateScenarioTool updates port data within a scenario
type UpdateScenarioTool struct{}

func NewUpdateScenarioTool() *UpdateScenarioTool {
	return &UpdateScenarioTool{}
}

func (t *UpdateScenarioTool) Name() string {
	return "update_scenario"
}

func (t *UpdateScenarioTool) Description() string {
	return `Update sample data for a specific port within a scenario. Use this to customize scenario data — for example, changing HTTP headers, request body, or any port's sample payload.

The port name must be the full port identifier (e.g., "flowid.module.component-suffix:portname"). Use get_node_port_schema to find port names, or list_scenarios to see available scenarios.

Example:
  update_scenario(resource_name: "trace-abc123xyz", port: "8740cbfb.tinysystems-http-module-v0.http-server-gvj48:request", data: {"method": "POST", "host": "example.com", "body": "test"})`
}

func (t *UpdateScenarioTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"resource_name": map[string]interface{}{
				"type":        "string",
				"description": "Scenario resource name (from list_scenarios)",
			},
			"port": map[string]interface{}{
				"type":        "string",
				"description": "Full port name (e.g., 'flowid.module.component-suffix:portname')",
			},
			"data": map[string]interface{}{
				"type":        "object",
				"description": "JSON data to set as the port's sample payload",
			},
		},
		"required": []string{"resource_name", "port", "data"},
	}
}

func (t *UpdateScenarioTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ScenarioManager == nil {
		return ToolResult{Success: false, Error: "scenario manager not configured"}
	}

	resourceName, _ := input["resource_name"].(string)
	if resourceName == "" {
		return ToolResult{Success: false, Error: "resource_name is required"}
	}

	port, _ := input["port"].(string)
	if port == "" {
		return ToolResult{Success: false, Error: "port is required"}
	}

	data, ok := input["data"]
	if !ok || data == nil {
		return ToolResult{Success: false, Error: "data is required"}
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to marshal data: %v", err)}
	}

	if err := execCtx.ScenarioManager.UpdateScenarioPort(ctx, execCtx.ProjectName, resourceName, port, dataBytes); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to update scenario port: %v", err)}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"resource_name": resourceName,
			"port":          port,
			"status":        "port data updated",
		},
	}
}

var _ Tool = (*UpdateScenarioTool)(nil)
