package tools

import (
	"context"
	"fmt"

	"github.com/tiny-systems/module/api/v1alpha1"
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

The widget defaults to the node's _control form, which is the form tiny and the
platform render.

Placement: a pinned widget lands on the project's first dashboard page,
appended below what is already there. Name a page to put it elsewhere (see
dashboard_page), give it a title so the user reads a label instead of a node
id, and set grid to lay the page out deliberately. Widths are in grid columns —
tiny and the platform render six — and a widget defaults to the full width.`
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
				"description": "Port to expose. Defaults to _control, and hosts that render only the control form will refuse anything else.",
			},
			"enabled": map[string]interface{}{
				"type":        "boolean",
				"description": "true (default) to pin the widget, false to remove it.",
			},
			"page": map[string]interface{}{
				"type":        "string",
				"description": "Dashboard page to place it on — resource name or title (from dashboard_page). Default: the project's first page, created if there is none.",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Label shown on the widget. Without one the user reads a node id.",
			},
			"grid": map[string]interface{}{
				"type":        "object",
				"description": "Placement on the 6-column grid. Omit to append below the page's current content at full width.",
				"properties": map[string]interface{}{
					"x": map[string]interface{}{"type": "integer", "description": "Column, 0-5."},
					"y": map[string]interface{}{"type": "integer", "description": "Row. Omit to append below what is already on the page."},
					"w": map[string]interface{}{"type": "integer", "description": "Width in columns, 1-6. Default 6."},
					"h": map[string]interface{}{"type": "integer", "description": "Height in rows. Default 6."},
				},
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
		port = v1alpha1.ControlPort
	}
	enabled := true
	if v, ok := input["enabled"].(bool); ok {
		enabled = v
	}
	pageArg, _ := input["page"].(string)
	title, _ := input["title"].(string)

	if _, err := execCtx.DashboardWriter.SetNodeWidget(ctx, execCtx.ProjectName, nodeID, port, enabled); err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}

	// The label says the node is a widget; the placement says where it sits.
	// Both, or the widget exists with nowhere to be — which is how it ends up
	// invisible on a dashboard that was supposed to show it.
	placement := WidgetPlacement{
		Page:   pageArg,
		NodeID: nodeID,
		Port:   port,
		Title:  title,
		Remove: !enabled,
	}
	if !enabled {
		placement.Page = "" // off every page
	} else {
		placement.X, placement.Y, placement.W, placement.H, placement.AutoY = gridOf(input)
	}

	page, err := execCtx.DashboardWriter.PlaceWidget(ctx, execCtx.ProjectName, placement)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}

	out := map[string]interface{}{
		"node_id": nodeID,
		"port":    port,
		"enabled": enabled,
		"page":    page.Title,
		"page_id": page.Name,
		"widgets": len(page.Widgets),
	}
	if enabled {
		out["hint"] = fmt.Sprintf("Widget pinned on page %q. If it renders 'Object is empty', the node's settings.context needs a configurable schema.", page.Title)
	}
	return ToolResult{Success: true, Output: out}
}

// gridOf reads the optional grid object. A missing y means "append below what
// is there", which is what an agent wants when it is adding one more widget
// rather than laying out a page.
func gridOf(input map[string]interface{}) (x, y, w, h int, autoY bool) {
	grid, ok := input["grid"].(map[string]interface{})
	if !ok {
		return 0, 0, 0, 0, true
	}
	num := func(key string) (int, bool) {
		switch v := grid[key].(type) {
		case float64:
			return int(v), true
		case int:
			return v, true
		}
		return 0, false
	}
	x, _ = num("x")
	w, _ = num("w")
	h, _ = num("h")
	y, hasY := num("y")
	return x, y, w, h, !hasY
}
