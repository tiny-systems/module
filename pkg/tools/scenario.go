package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ScenariosTool is the single entry point for scenario CRUD. Scenarios are
// sample port data snapshots used for compile-time edge validation —
// either captured from a real trace or hand-populated.
//
// One action per call. Fields used vary by action — see Description.
type ScenariosTool struct{}

func NewScenariosTool() *ScenariosTool {
	return &ScenariosTool{}
}

func (t *ScenariosTool) Name() string {
	return "scenarios"
}

func (t *ScenariosTool) Description() string {
	return `Manage scenarios (sample port data snapshots for compile-time edge validation).

Actions and their required fields:

- action="list":
  Returns all scenarios in the current project.

- action="create": name
  Optional: trace_id (capture real data from a previous send_signal execution).
  Creates a new scenario, either from a trace (recommended when available) or empty.

- action="update": resource_name, port, data
  Sets sample data for a specific port within an existing scenario.
  Port name is the full port identifier (e.g., "flowid.module.component-suffix:portname").

- action="delete": resource_name
  Removes the scenario by its resource_name (from list).

Examples:
  scenarios(action: "list")
  scenarios(action: "create", name: "Happy path", trace_id: "abc123")
  scenarios(action: "update", resource_name: "trace-abc", port: "flow.module.comp-1:request", data: {"method":"POST"})
  scenarios(action: "delete", resource_name: "trace-abc")`
}

func (t *ScenariosTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"list", "create", "update", "delete"},
				"description": "Operation to perform. Other fields depend on this value — see tool description.",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "(create) Human-readable name for the scenario.",
			},
			"trace_id": map[string]interface{}{
				"type":        "string",
				"description": "(create, optional) Trace id to capture data from. Omit to create empty.",
			},
			"resource_name": map[string]interface{}{
				"type":        "string",
				"description": "(update, delete) Scenario resource name from action=list.",
			},
			"port": map[string]interface{}{
				"type":        "string",
				"description": "(update) Full port name (e.g., 'flowid.module.component-suffix:portname').",
			},
			"data": map[string]interface{}{
				"type":        "object",
				"description": "(update) JSON data to set as the port's sample payload.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ScenariosTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ScenarioManager == nil {
		return ToolResult{Success: false, Error: "scenario manager not configured"}
	}
	action, _ := input["action"].(string)
	switch action {
	case "list":
		return scenariosList(ctx, execCtx)
	case "create":
		return scenariosCreate(ctx, execCtx, input)
	case "update":
		return scenariosUpdate(ctx, execCtx, input)
	case "delete":
		return scenariosDelete(ctx, execCtx, input)
	default:
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("unknown action %q; expected list, create, update, delete", action),
		}
	}
}

func scenariosList(ctx context.Context, execCtx ExecutionContext) ToolResult {
	items, err := execCtx.ScenarioManager.ListScenarios(ctx, execCtx.ProjectName)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to list scenarios: %v", err)}
	}
	out := map[string]interface{}{
		"scenarios": items,
		"total":     len(items),
	}
	if len(items) == 0 {
		out["hint"] = "No scenarios yet. Use scenarios(action: create, name, trace_id) after a send_signal execution, or scenarios(action: create, name) for an empty one."
	}
	return ToolResult{Success: true, Output: out}
}

func scenariosCreate(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	name, _ := input["name"].(string)
	if name == "" {
		return ToolResult{Success: false, Error: "name is required for create"}
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

func scenariosUpdate(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	resourceName, _ := input["resource_name"].(string)
	if resourceName == "" {
		return ToolResult{Success: false, Error: "resource_name is required for update"}
	}
	port, _ := input["port"].(string)
	if port == "" {
		return ToolResult{Success: false, Error: "port is required for update"}
	}
	data, ok := input["data"]
	if !ok || data == nil {
		return ToolResult{Success: false, Error: "data is required for update"}
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

func scenariosDelete(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	resourceName, _ := input["resource_name"].(string)
	if resourceName == "" {
		return ToolResult{Success: false, Error: "resource_name is required for delete"}
	}
	if err := execCtx.ScenarioManager.DeleteScenario(ctx, execCtx.ProjectName, resourceName); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to delete scenario: %v", err)}
	}
	return ToolResult{Success: true, Output: map[string]interface{}{"deleted": resourceName}}
}

var _ Tool = (*ScenariosTool)(nil)
