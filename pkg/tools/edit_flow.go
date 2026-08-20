package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
  Optional: configuration (+ schema) — pass them to wire AND configure in one call
  (same mapping/schema rules as configure_edge). Omit to just create the wire and
  get back the target port schema to configure next.
  Returns edge_id and whether configuration is still needed.

- action="delete_edge": edge_id
  Removes an edge.

- action="configure_edge": edge_id, configuration (object, JSON-Schema-like for data mapping)
  Optional: schema (JSON-Schema overrides), trace_id (validate against real data).
  Configures how data maps from source to target port.

- action="share_node": node_id, flows (array of flow names)
  Shares the node into those flows of the same project, so their canvases can wire to
  it. Flows are layers of one picture; a node still belongs to the flow that owns it,
  keeps one position, and stays read-only elsewhere. The list REPLACES the current
  one — pass [] to un-share.

- action="configure_node": node_id, and settings, position and/or label
  label names the node — it is what the canvas shows and what a dashboard widget
  is titled. Name what a node DOES ("Latest pod summary"), not what it is ("Display").
  settings (object, the _settings port configuration) may add/remove output ports (e.g. router routes).
  position ({x, y}) moves the node on the canvas — pass it alone to tidy a layout
  without touching configuration. See the layout rules in the flow-building guide.
  Optional: schema (JSON-Schema overrides for configurable fields).

Examples:
  edit_flow(action: "add_node", component: "router", module: "tinysystems/common-module-v1")
  edit_flow(action: "delete_node", node_id: "router-abc123")
  edit_flow(action: "add_edge", from_node: "server-abc", from_port: "request", to_node: "logger-def", to_port: "input")
  edit_flow(action: "configure_edge", edge_id: "edge-xyz", configuration: {"data": "{{$.body}}"})
  edit_flow(action: "configure_node", node_id: "router-abc", position: {x: 940, y: 260})
  edit_flow(action: "share_node", node_id: "kv-abc", flows: ["watch", "setup"])`
}

func (t *EditFlowTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"add_node", "delete_node", "add_edge", "delete_edge", "configure_edge", "configure_node", "share_node"},
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
				"description": "(delete_node, configure_node, share_node) Target node id.",
			},
			"flows": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "(share_node) Flow names to share this node into. Replaces the current set; [] un-shares.",
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
				"description": "(add_edge, configure_edge) Data mapping using {{expression}} syntax. Object or JSON string. On add_edge it's applied to the new edge in the same call.",
			},
			"settings": map[string]interface{}{
				"type":        "object",
				"description": "(configure_node) Settings object for the _settings port. Object or JSON string.",
			},
			"label": map[string]interface{}{
				"type":        "string",
				"description": "(configure_node) The node's name, shown on the canvas and used as its dashboard widget's title. Name what it does, not what it is.",
			},
			"position": map[string]interface{}{
				"type":        "object",
				"description": "(configure_node) Canvas position {x, y} in pixels. Pass it alone to move a node without changing its configuration — use it to fix a cramped or misleading layout after a flow is built.",
			},
			"schema": map[string]interface{}{
				"type":        "object",
				"description": "(add_edge, configure_edge, configure_node) Optional JSON-Schema overrides for configurable fields. Required when configuration fills a configurable field like context.",
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
		return scaffoldAfterEdgeChange(ctx, execCtx, editFlowAddEdge(ctx, execCtx, input))
	case "delete_edge":
		return editFlowDeleteEdge(ctx, execCtx, input)
	case "configure_edge":
		return scaffoldAfterEdgeChange(ctx, execCtx, editFlowConfigureEdge(ctx, execCtx, input))
	case "configure_node":
		return editFlowConfigureNode(ctx, execCtx, input)
	case "share_node":
		return editFlowShareNode(ctx, execCtx, input)
	default:
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("unknown action %q; expected add_node, delete_node, add_edge, delete_edge, configure_edge, configure_node, share_node", action),
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

// nodeHasPublishedPorts reports whether a node has reconciled far enough to
// have published its ports, which is what makes "this port does not exist"
// provable rather than merely unobserved. Probed through the settings port
// because every component publishes one and there is no list-ports call;
// a node mid-reconcile answers for no port at all.
//
// Fails open: if the probe cannot confirm the node is published, callers skip
// blocking. A missed wiring mistake is recoverable, refusing a correct edge is
// not — build_flow creates nodes and edges in one call, and the nodes may still
// be reconciling when the edges are wired.
func nodeHasPublishedPorts(ctx context.Context, execCtx ExecutionContext, nodeID string) bool {
	if execCtx.PortInspector == nil {
		return false
	}
	_, err := execCtx.PortInspector.InspectPort(ctx, execCtx.ProjectName, nodeID, "_settings", "")
	return err == nil
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

	// Refuse to wire a port that does not exist, BEFORE creating the edge. Such
	// an edge is not merely invalid, it is dangling: it simulates to nothing, so
	// validation reports a downstream type error that blames the mapping and
	// never names the real cause, and at runtime a source node holding an edge
	// on a port nobody emits from stalls the flow. Creating it and then
	// reporting "invalid" left the wreckage behind.
	//
	// A node that has not reconciled yet publishes no ports at all; InspectPort
	// fails for every port name then, so only block when the node HAS ports and
	// this one is not among them.
	if execCtx.PortInspector != nil {
		for _, end := range []struct {
			node, port, side, kind string
		}{
			{fromNode, fromPort, "source", "output"},
			{toNode, toPort, "target", "input"},
		} {
			if _, err := execCtx.PortInspector.InspectPort(ctx, execCtx.ProjectName, end.node, end.port, ""); err == nil {
				continue
			}
			if !nodeHasPublishedPorts(ctx, execCtx, end.node) {
				continue // not reconciled — cannot prove the port absent
			}
			return ToolResult{
				Success: false,
				Error: fmt.Sprintf("%s port %q does not exist on node %s — confirm the component's %s ports with get_component_info",
					end.side, end.port, end.node, end.kind),
			}
		}
	}

	result, err := execCtx.EdgeAdder.AddEdge(ctx, execCtx.ProjectName, execCtx.FlowName, fromNode, fromPort, toNode, toPort)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to add edge: %s", err.Error())}
	}

	// One-call add+configure: if the caller supplied a configuration inline,
	// apply it to the freshly-created edge instead of ignoring it. The old
	// behavior accepted `configuration`/`schema` (they're valid JSON) but never
	// applied them — shipping an unconfigured edge and forcing a second
	// configure_edge round-trip, which reads as "carried config silently
	// dropped". Reuse the configure_edge path verbatim (schema-required check,
	// validation, hint surfacing) by injecting the new edge id.
	if cfg := input["configuration"]; cfg != nil {
		if _, ok := cfg.(map[string]interface{}); ok {
		} else if _, ok := cfg.(string); !ok {
			cfg = nil // not a map or JSON string — ignore, fall through to add-only
		}
		if cfg != nil {
			input["edge_id"] = result.EdgeID
			res := editFlowConfigureEdge(ctx, execCtx, input)
			outMap, _ := res.Output.(map[string]interface{})
			if outMap == nil {
				outMap = map[string]interface{}{}
			}
			outMap["edge_id"] = result.EdgeID
			outMap["edge_created"] = true
			res.Output = outMap
			return res
		}
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

	// Derive schemas for filled configurable fields the caller didn't
	// describe — mirrors the build_flow pass. Without the build spec in
	// hand there is no source example to resolve expressions against, so
	// expression-valued keys stay `{}` (untyped, never wrong) while
	// literals keep their real types. Best-effort: when project state or
	// module catalog can't be reached, the schema stays as supplied.
	if len(config) > 0 {
		if targetNodeID, targetPort, _ := findEdgeTarget(ctx, execCtx, edgeID); targetNodeID != "" {
			if comp, _ := findNodeComponent(ctx, execCtx, targetNodeID); comp != nil {
				targetSchema := portSchemaBytes(comp, targetPort, true)
				edgeSchema = fillMissingSchemas(config, edgeSchema, configurableFieldsIn(targetSchema), nil)
			}
		}
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
	// verified says whether the expressions were actually RESOLVED against
	// sample data. valid:true with a hint means they never were — the edge is
	// unfinished work, not a pass. Scenarios are the fix: scaffold
	// placeholders first, then pin a real trace.
	out := map[string]interface{}{"valid": true, "edge_id": edgeID, "verified": result.Hint == ""}
	if result.Hint != "" {
		// A valid edge can still carry an advisory (e.g. it maps a field the
		// target port doesn't declare, which persists but is dropped at runtime).
		// Surface it — dropping it here is what let silent data loss ship green.
		out["hint"] = result.Hint
	}
	return ToolResult{Success: true, Output: out}
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

	// Moving a node changes nothing about what the flow does, so a reposition
	// stands on its own: requiring settings alongside it would force a caller
	// tidying a layout to resend configuration it has no reason to touch.
	label, hasLabel := input["label"].(string)
	if hasLabel {
		if execCtx.NodeLabeler == nil {
			return ToolResult{Success: false, Error: "naming a node is not supported here"}
		}
		if err := execCtx.NodeLabeler.LabelNode(ctx, execCtx.ProjectName, execCtx.FlowName, nodeID, label); err != nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("failed to name node: %s", err.Error())}
		}
	}

	x, y, hasPosition := positionFrom(input["position"])
	if hasPosition {
		if execCtx.NodeRepositioner == nil {
			return ToolResult{Success: false, Error: "repositioning is not supported here"}
		}
		if err := execCtx.NodeRepositioner.RepositionNode(ctx, execCtx.ProjectName, execCtx.FlowName, nodeID, x, y); err != nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("failed to reposition node: %s", err.Error())}
		}
	}

	settings, settingsOk := input["settings"].(map[string]interface{})
	if !settingsOk {
		if settingsStr, isString := input["settings"].(string); isString {
			if err := json.Unmarshal([]byte(settingsStr), &settings); err != nil {
				return ToolResult{Success: false, Error: fmt.Sprintf("settings string is not valid JSON: %v", err)}
			}
		} else if hasPosition || hasLabel {
			// Nothing left to configure beyond what was already applied.
			out := map[string]interface{}{"node_id": nodeID}
			if hasPosition {
				out["position"] = map[string]interface{}{"x": x, "y": y}
			}
			if hasLabel {
				out["label"] = label
			}
			return ToolResult{Success: true, Output: out}
		} else {
			return ToolResult{Success: false, Error: "configure_node needs settings, a position, a label, or any combination"}
		}
	}

	// Derive schemas for filled configurable settings the caller didn't
	// describe — settings are literal data, so value types are ground
	// truth (this is NOT the removed template-string inference). User
	// entries win. Best-effort: when the cluster state can't be reached,
	// the schema stays as supplied.
	if len(settings) > 0 {
		if comp, _ := findNodeComponent(ctx, execCtx, nodeID); comp != nil {
			schemaBytes := portSchemaBytes(comp, "_settings", true)
			settingsSchema = fillMissingSchemas(settings, settingsSchema, configurableFieldsIn(schemaBytes), nil)
		}
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
	if result.Hint != "" {
		if existing, ok := output["hint"].(string); ok && existing != "" {
			output["hint"] = existing + " " + result.Hint
		} else {
			output["hint"] = result.Hint
		}
	}
	return ToolResult{Success: true, Output: output}
}

var _ Tool = (*EditFlowTool)(nil)

// positionFrom reads a {x, y} position from tool input, accepting either an
// object or a JSON string — the same latitude the other inputs allow, since a
// caller that serialises one field tends to serialise them all.
//
// Both coordinates must be present: a half-given position would move a node
// somewhere nobody asked for, which is harder to notice than a rejection.
func positionFrom(raw interface{}) (int, int, bool) {
	if raw == nil {
		return 0, 0, false
	}
	pos, ok := raw.(map[string]interface{})
	if !ok {
		s, isString := raw.(string)
		if !isString {
			return 0, 0, false
		}
		if err := json.Unmarshal([]byte(s), &pos); err != nil {
			return 0, 0, false
		}
	}
	x, xOk := numberFrom(pos["x"])
	y, yOk := numberFrom(pos["y"])
	if !xOk || !yOk {
		return 0, 0, false
	}
	return x, y, true
}

// numberFrom accepts the shapes JSON decoding produces for an integer.
func numberFrom(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// scaffoldAfterEdgeChange tops up the auto-scaffold scenario after an edge is
// added or reconfigured, the way build_flow already does at the end of a
// build. Without it only build_flow-created edges ever got sample data, so a
// flow an agent rewired incrementally — the normal case after the first
// build — validated against nothing and shipped red.
//
// Best-effort by the same rule as the build path: a scaffold failure appends
// a warning and never turns a successful edit into an error. Placeholders are
// only written where a port has no sample yet, so a real trace-derived
// scenario is never overwritten.
func scaffoldAfterEdgeChange(ctx context.Context, execCtx ExecutionContext, res ToolResult) ToolResult {
	if !res.Success || execCtx.ScenarioManager == nil || execCtx.TinyNodeCRManager == nil {
		return res
	}
	written, warnings := ScaffoldLiveScenarios(ctx, execCtx)
	if written == 0 && len(warnings) == 0 {
		return res
	}
	out, ok := res.Output.(map[string]interface{})
	if !ok {
		return res
	}
	if written > 0 {
		out["scenario_ports_scaffolded"] = written
	}
	if len(warnings) > 0 {
		out["scenario_warnings"] = warnings
	}
	res.Output = out
	return res
}

// editFlowShareNode shares a node into other layers of the same project.
func editFlowShareNode(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.NodeSharer == nil {
		return ToolResult{Success: false, Error: "sharing a node is not supported here"}
	}
	nodeID, _ := input["node_id"].(string)
	if nodeID == "" {
		return ToolResult{Success: false, Error: "node_id is required for share_node"}
	}
	flows, ok := stringsFrom(input["flows"])
	if !ok {
		return ToolResult{Success: false, Error: "flows must be an array of flow names for share_node (pass [] to un-share)"}
	}
	result, err := execCtx.NodeSharer.ShareNode(ctx, execCtx.ProjectName, execCtx.FlowName, nodeID, flows)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to share node: %s", err.Error())}
	}
	out := map[string]interface{}{"node_id": nodeID, "flows": result.Flows}
	if len(result.Flows) == 0 {
		out["hint"] = "Node is no longer shared; only its own flow can see it."
	} else {
		out["hint"] = "Those flows can now wire to this node. It stays read-only there — it moves and configures only in the flow that owns it."
	}
	return ToolResult{Success: true, Output: out}
}

// stringsFrom accepts an array of strings, a JSON array in a string, or a
// comma-separated string. An explicit empty array is valid input — it is how
// a node is un-shared — so absence and emptiness are answered differently:
// a missing key is not a list at all.
func stringsFrom(raw interface{}) ([]string, bool) {
	switch v := raw.(type) {
	case []string:
		return v, true
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return []string{}, true
		}
		if strings.HasPrefix(trimmed, "[") {
			var out []string
			if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
				return nil, false
			}
			return out, true
		}
		parts := strings.Split(trimmed, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out, true
	}
	return nil, false
}
