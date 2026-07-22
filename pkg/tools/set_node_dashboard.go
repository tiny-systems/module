package tools

import (
	"context"
	"fmt"
)

// SetNodeDashboardTool pins a node's control form to the project dashboard as a
// widget, or unpins it.
//
// This is how a flow gets a user-facing surface. It matters most for
// credentials: the canonical pattern is that the user types everything the flow
// needs into a widget at start-up, with secret fields declared secret:true so
// they render masked. Build the node and its schema but never pin it and the
// flow is one a user can look at but not run.
type SetNodeDashboardTool struct{}

func NewSetNodeDashboardTool() *SetNodeDashboardTool {
	return &SetNodeDashboardTool{}
}

func (t *SetNodeDashboardTool) Name() string { return "set_node_dashboard" }

func (t *SetNodeDashboardTool) Description() string {
	return `Pin a node's control form to the project dashboard as a widget (or unpin it).

This is the flow's user-facing surface — a flow with no widgets is one the user
can see but cannot run. Pin the trigger (signal/cron/ticker) so they can start
it, and any node whose values they must supply.

Credentials: the user types them into the widget. Declare the field in the
node's settings_schema with secret:true so it renders masked, and do NOT
provision a Kubernetes Secret for an individual flow — modules are shared across
many flows, so per-flow secrets don't scale.

The widget renders as a form only if the node's settings.context has a matching
configurable schema; without it the widget shows "Object is empty". Configure
the node first, then pin it.

Defaults to the _control port, which is the form the dashboard renders.`
}

func (t *SetNodeDashboardTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"node_id": map[string]interface{}{
				"type":        "string",
				"description": "Full node id to pin, e.g. <flow>.common-module-v0.signal-xxxx (from read_project).",
			},
			"port": map[string]interface{}{
				"type":        "string",
				"description": "Port to expose. Default _control — the node's control form.",
			},
			"enabled": map[string]interface{}{
				"type":        "boolean",
				"description": "true (default) to pin the widget, false to remove it.",
			},
		},
		"required": []string{"node_id"},
	}
}

func (t *SetNodeDashboardTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.DashboardWriter == nil {
		return ToolResult{Success: false, Error: "dashboard writer not configured"}
	}

	nodeID, _ := input["node_id"].(string)
	if nodeID == "" {
		return ToolResult{Success: false, Error: "node_id is required"}
	}
	port, _ := input["port"].(string)
	if port == "" {
		port = "_control"
	}
	enabled := true
	if v, ok := input["enabled"].(bool); ok {
		enabled = v
	}

	page, err := execCtx.DashboardWriter.SetNodeWidget(ctx, execCtx.ProjectName, nodeID, port, enabled)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}

	out := map[string]interface{}{
		"node_id": nodeID,
		"port":    port,
		"enabled": enabled,
		"page":    page,
	}
	if enabled {
		out["hint"] = fmt.Sprintf("Widget pinned on page %q. If it renders 'Object is empty', the node's settings.context needs a configurable schema.", page)
	}
	return ToolResult{Success: true, Output: out}
}
