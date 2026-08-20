package tools

import (
	"context"
	"fmt"
)

// DashboardPageTool manages the tabs a project's dashboard is divided into.
//
// A dashboard with one page holds everything in one column, which is fine for a
// flow with a trigger and a status readout and unreadable for anything larger.
// The editor has had pages since the beginning; an agent building the same
// project could pin widgets but not say where they went, so a project it
// assembled arrived as one undifferentiated list — a setup form beside a live
// log beside a button, in the order they happened to be created.
type DashboardPageTool struct{}

func NewDashboardPageTool() *DashboardPageTool {
	return &DashboardPageTool{}
}

func (t *DashboardPageTool) Name() string { return "dashboard_page" }

func (t *DashboardPageTool) Description() string {
	return `Manage the project dashboard's pages (the tabs across the top).

Actions:
  list   — every page in display order, with the widgets placed on each
  create — add a page with a title, returns the id to place widgets on
  delete — remove a page and its placements

Separate what a user does once from what they watch: a "Setup" page holding the
credential and configuration forms, and a page per running concern. Deleting a
page removes only the layout — the nodes behind those widgets keep running, and
their widgets can be placed again.

Place widgets with set_node_dashboard, naming the page.`
}

func (t *DashboardPageTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"list", "create", "delete"},
				"description": "list, create or delete.",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "For create: the tab label the user reads.",
			},
			"page": map[string]interface{}{
				"type":        "string",
				"description": "For delete: the page's resource name (from list), or its title.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *DashboardPageTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.DashboardWriter == nil {
		return ToolResult{Success: false, Error: "dashboard writer not configured"}
	}

	action, _ := input["action"].(string)
	switch action {
	case "list":
		pages, err := execCtx.DashboardWriter.ListPages(ctx, execCtx.ProjectName)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}
		}
		out := map[string]interface{}{"pages": pages, "count": len(pages)}
		if len(pages) == 0 {
			out["hint"] = "No pages yet. Pinning a widget creates the first one, or create it here to choose its title."
		}
		return ToolResult{Success: true, Output: out}

	case "create":
		title, _ := input["title"].(string)
		if title == "" {
			return ToolResult{Success: false, Error: "title is required to create a page — it is the tab label the user reads"}
		}
		page, err := execCtx.DashboardWriter.CreatePage(ctx, execCtx.ProjectName, title)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}
		}
		return ToolResult{Success: true, Output: map[string]interface{}{
			"page": page,
			"hint": fmt.Sprintf("Place widgets with set_node_dashboard, page: %q.", page.Name),
		}}

	case "delete":
		page, _ := input["page"].(string)
		if page == "" {
			return ToolResult{Success: false, Error: "page is required to delete — pass the resource name from list"}
		}
		if err := execCtx.DashboardWriter.DeletePage(ctx, execCtx.ProjectName, page); err != nil {
			return ToolResult{Success: false, Error: err.Error()}
		}
		return ToolResult{Success: true, Output: map[string]interface{}{
			"deleted": page,
			"hint":    "Layout only — the nodes behind those widgets are untouched and still running.",
		}}
	}

	return ToolResult{Success: false, Error: fmt.Sprintf("unknown action %q — use list, create or delete", action)}
}
