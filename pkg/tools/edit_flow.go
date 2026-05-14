package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tiny-systems/module/pkg/schema"
)

// EditFlowTool is the single entry point for incremental flow edits.
// Most flow construction should go through build_flow (full graph in one
// call). Use this when the model needs to fix something specific that
// build_flow didn't get right, or when iterating on an existing flow.
//
// One action per call. Fields used vary by action — see Description.
type EditFlowTool struct{}

func NewEditFlowTool() *EditFlowTool {
	return &EditFlowTool{}
}

func (t *EditFlowTool) Name() string {
	return "edit_flow"
}

func (t *EditFlowTool) Description() string {
	return `Incremental edit of the current flow. Prefer build_flow for full graphs; use edit_flow to fix specific things.

Actions and their required fields:

- action="add_node": component, module
  Adds a node. Returns node_id and available ports.

- action="delete_node": node_id
  Removes a node and its edges.

- action="add_edge": from_node, from_port, to_node, to_port
  Connects two ports. Returns edge_id and whether configuration is needed.

- action="delete_edge": edge_id
  Removes an edge.

- action="configure_edge": edge_id, configuration (object, JSON-Schema-like for data mapping)
  Optional: schema (JSON-Schema overrides), trace_id (validate against real data).
  Configures how data maps from source to target port.

- action="configure_node": node_id, settings (object, the _settings port configuration)
  Optional: schema (JSON-Schema overrides for configurable fields).
  Configures node-level settings. May add/remove output ports (e.g., router routes).

Examples:
  edit_flow(action: "add_node", component: "router", module: "tinysystems/common-module-v1")
  edit_flow(action: "delete_node", node_id: "router-abc123")
  edit_flow(action: "add_edge", from_node: "server-abc", from_port: "request", to_node: "logger-def", to_port: "input")
  edit_flow(action: "configure_edge", edge_id: "edge-xyz", configuration: {"data": "{{$.body}}"})`
}

func (t *EditFlowTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"add_node", "delete_node", "add_edge", "delete_edge", "configure_edge", "configure_node"},
				"description": "Operation to perform. Other fields depend on this value — see tool description.",
			},
			"component": map[string]interface{}{
				"type":        "string",
				"description": "(add_node) Exact component name from list_modules.",
			},
			"module": map[string]interface{}{
				"type":        "string",
				"description": "(add_node) Module name from list_modules.",
			},
			"node_id": map[string]interface{}{
				"type":        "string",
				"description": "(delete_node, configure_node) Target node id.",
			},
			"edge_id": map[string]interface{}{
				"type":        "string",
				"description": "(delete_edge, configure_edge) Target edge id.",
			},
			"from_node": map[string]interface{}{
				"type":        "string",
				"description": "(add_edge) Source node id.",
			},
			"from_port": map[string]interface{}{
				"type":        "string",
				"description": "(add_edge) Source port name.",
			},
			"to_node": map[string]interface{}{
				"type":        "string",
				"description": "(add_edge) Target node id.",
			},
			"to_port": map[string]interface{}{
				"type":        "string",
				"description": "(add_edge) Target port name.",
			},
			"configuration": map[string]interface{}{
				"type":        "object",
				"description": "(configure_edge) Data mapping using {{expression}} syntax. Object or JSON string.",
			},
			"settings": map[string]interface{}{
				"type":        "object",
				"description": "(configure_node) Settings object for the _settings port. Object or JSON string.",
			},
			"schema": map[string]interface{}{
				"type":        "object",
				"description": "(configure_edge, configure_node) Optional JSON-Schema overrides for configurable fields.",
			},
			"trace_id": map[string]interface{}{
				"type":        "string",
				"description": "(configure_edge) Optional trace id to validate the configuration against real execution data.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *EditFlowTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	action, _ := input["action"].(string)
	switch action {
	case "add_node":
		return editFlowAddNode(ctx, execCtx, input)
	case "delete_node":
		return editFlowDeleteNode(ctx, execCtx, input)
	case "add_edge":
		return editFlowAddEdge(ctx, execCtx, input)
	case "delete_edge":
		return editFlowDeleteEdge(ctx, execCtx, input)
	case "configure_edge":
		return editFlowConfigureEdge(ctx, execCtx, input)
	case "configure_node":
		return editFlowConfigureNode(ctx, execCtx, input)
	default:
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("unknown action %q; expected add_node, delete_node, add_edge, delete_edge, configure_edge, configure_node", action),
		}
	}
}

func editFlowAddNode(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.NodeAdder == nil {
		return ToolResult{Success: false, Error: "node adder not configured"}
	}

	component, _ := input["component"].(string)
	module, _ := input["module"].(string)
	if component == "" {
		return ToolResult{Success: false, Error: "component is required for add_node"}
	}
	if module == "" {
		return ToolResult{Success: false, Error: "module is required for add_node"}
	}

	if execCtx.ModuleCatalog != nil {
		moduleInfo, err := execCtx.ModuleCatalog.GetModule(ctx, module)
		if err != nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("failed to lookup module: %s", err.Error())}
		}
		if moduleInfo == nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("module %q not found. Use list_modules to see available modules.", module)}
		}
		var availableComponents []string
		componentFound := false
		for _, c := range moduleInfo.Components {
			availableComponents = append(availableComponents, c.Name)
			if c.Name == component {
				componentFound = true
				break
			}
		}
		if !componentFound {
			return ToolResult{Success: false, Error: fmt.Sprintf("component %q not found in module %q. Available: %v", component, module, availableComponents)}
		}
	}

	result, err := execCtx.NodeAdder.AddNode(ctx, execCtx.ProjectName, execCtx.FlowName, component, module, execCtx.PositionTracker)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to add node: %s", err.Error())}
	}
	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"node_id": result.NodeID,
			"ports":   result.Ports,
			"hint":    "Use edit_flow with action=add_edge to connect this node to other nodes.",
		},
	}
}

func editFlowDeleteNode(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.FlowModifier == nil {
		return ToolResult{Success: false, Error: "flow modifier not configured"}
	}
	nodeID, _ := input["node_id"].(string)
	if nodeID == "" {
		return ToolResult{Success: false, Error: "node_id is required for delete_node"}
	}
	ops := []FlowOperation{{Op: "delete", ID: nodeID, Element: map[string]interface{}{"type": "tinyNode"}}}
	results, err := execCtx.FlowModifier.ApplyFlowChanges(ctx, execCtx.ProjectName, execCtx.FlowName, ops)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to delete node: %s", err.Error())}
	}
	if len(results) == 0 || !results[0].Success {
		errMsg := "unknown error"
		if len(results) > 0 && results[0].Error != "" {
			errMsg = results[0].Error
		}
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to delete node: %s", errMsg)}
	}
	return ToolResult{Success: true, Output: map[string]interface{}{"deleted": true, "node_id": nodeID}}
}

func editFlowAddEdge(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.EdgeAdder == nil {
		return ToolResult{Success: false, Error: "edge adder not configured"}
	}
	fromNode, _ := input["from_node"].(string)
	fromPort, _ := input["from_port"].(string)
	toNode, _ := input["to_node"].(string)
	toPort, _ := input["to_port"].(string)
	if fromNode == "" || fromPort == "" {
		return ToolResult{Success: false, Error: "from_node and from_port are required for add_edge"}
	}
	if toNode == "" || toPort == "" {
		return ToolResult{Success: false, Error: "to_node and to_port are required for add_edge"}
	}

	result, err := execCtx.EdgeAdder.AddEdge(ctx, execCtx.ProjectName, execCtx.FlowName, fromNode, fromPort, toNode, toPort)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to add edge: %s", err.Error())}
	}

	output := map[string]interface{}{
		"edge_id":             result.EdgeID,
		"needs_configuration": result.NeedsConfiguration,
	}
	if result.NeedsConfiguration {
		output["hint"] = "Use edit_flow with action=configure_edge and configuration matching the target_port_schema below."
		if execCtx.PortInspector != nil {
			targetPortInfo, err := execCtx.PortInspector.InspectPort(ctx, execCtx.ProjectName, toNode, toPort, "")
			if err == nil && targetPortInfo != nil {
				output["target_port_schema"] = targetPortInfo.Schema
				if targetPortInfo.ExampleData != nil {
					output["target_port_example"] = targetPortInfo.ExampleData
				}
			}
		}
	}
	return ToolResult{Success: true, Output: output}
}

func editFlowDeleteEdge(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.FlowModifier == nil {
		return ToolResult{Success: false, Error: "flow modifier not configured"}
	}
	edgeID, _ := input["edge_id"].(string)
	if edgeID == "" {
		return ToolResult{Success: false, Error: "edge_id is required for delete_edge"}
	}
	ops := []FlowOperation{{Op: "delete", ID: edgeID, Element: map[string]interface{}{"type": "tinyEdge"}}}
	results, err := execCtx.FlowModifier.ApplyFlowChanges(ctx, execCtx.ProjectName, execCtx.FlowName, ops)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to delete edge: %s", err.Error())}
	}
	if len(results) == 0 || !results[0].Success {
		errMsg := "unknown error"
		if len(results) > 0 && results[0].Error != "" {
			errMsg = results[0].Error
		}
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to delete edge: %s", errMsg)}
	}
	return ToolResult{Success: true, Output: map[string]interface{}{"deleted": true, "edge_id": edgeID}}
}

func editFlowConfigureEdge(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.EdgeConfigurer == nil {
		return ToolResult{Success: false, Error: "edge configurer not configured"}
	}
	edgeID, _ := input["edge_id"].(string)
	edgeSchema, _ := input["schema"].(map[string]interface{})
	traceID, _ := input["trace_id"].(string)
	if edgeID == "" {
		return ToolResult{Success: false, Error: "edge_id is required for configure_edge"}
	}

	config, configOk := input["configuration"].(map[string]interface{})
	if !configOk {
		if configStr, isString := input["configuration"].(string); isString {
			if err := json.Unmarshal([]byte(configStr), &config); err != nil {
				return ToolResult{Success: false, Error: fmt.Sprintf("configuration string is not valid JSON: %v", err)}
			}
		} else {
			return ToolResult{Success: false, Error: "configuration is required for configure_edge and must be a JSON object or JSON string"}
		}
	}

	// If no schema was supplied, infer one from the configuration shape so
	// LLM callers don't have to ship data + schema as two synchronised
	// arguments. Explicit schema always wins.
	if edgeSchema == nil && len(config) > 0 {
		edgeSchema = schema.InferFromInstance(config)
	}

	result, err := execCtx.EdgeConfigurer.ConfigureEdge(ctx, execCtx.ProjectName, execCtx.FlowName, edgeID, config, edgeSchema, traceID)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to configure edge: %s", err.Error())}
	}
	if !result.Valid {
		output := map[string]interface{}{"valid": false, "error": result.Error}
		if result.Hint != "" {
			output["hint"] = result.Hint
		}
		return ToolResult{Success: false, Error: result.Error, Output: output}
	}
	return ToolResult{Success: true, Output: map[string]interface{}{"valid": true, "edge_id": edgeID}}
}

func editFlowConfigureNode(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.NodeSettingsConfigurer == nil {
		return ToolResult{Success: false, Error: "node settings configurer not configured"}
	}
	nodeID, _ := input["node_id"].(string)
	settingsSchema, _ := input["schema"].(map[string]interface{})
	if nodeID == "" {
		return ToolResult{Success: false, Error: "node_id is required for configure_node"}
	}

	settings, settingsOk := input["settings"].(map[string]interface{})
	if !settingsOk {
		if settingsStr, isString := input["settings"].(string); isString {
			if err := json.Unmarshal([]byte(settingsStr), &settings); err != nil {
				return ToolResult{Success: false, Error: fmt.Sprintf("settings string is not valid JSON: %v", err)}
			}
		} else {
			return ToolResult{Success: false, Error: "settings is required for configure_node and must be a JSON object or JSON string"}
		}
	}

	// Same fallback as configure_edge: infer schema from data shape when
	// the caller didn't supply one. Explicit schema wins.
	if settingsSchema == nil && len(settings) > 0 {
		settingsSchema = schema.InferFromInstance(settings)
	}

	result, err := execCtx.NodeSettingsConfigurer.ConfigureNodeSettings(ctx, execCtx.ProjectName, execCtx.FlowName, nodeID, settings, settingsSchema)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to configure node settings: %s", err.Error())}
	}
	if !result.Valid {
		output := map[string]interface{}{"valid": false, "error": result.Error}
		if result.Hint != "" {
			output["hint"] = result.Hint
		}
		return ToolResult{Success: false, Error: result.Error, Output: output}
	}

	output := map[string]interface{}{"valid": true, "node_id": nodeID}
	if len(result.Ports) > 0 {
		output["ports"] = result.Ports
		output["hint"] = "Settings may have changed available ports. Use these port names for edit_flow add_edge."
	}
	return ToolResult{Success: true, Output: output}
}

var _ Tool = (*EditFlowTool)(nil)
