package tools

import (
	"context"
	"fmt"
)

// ListProjectsTool returns the projects accessible in the current context.
// MCP-server reads TinyProject CRDs in the configured namespace; the
// platform reads the workspace's projects from its database. Either way
// the tool surfaces "what project resource names can I pass to other
// tools" without requiring the user to drop into kubectl or the UI.
type ListProjectsTool struct{}

func NewListProjectsTool() *ListProjectsTool {
	return &ListProjectsTool{}
}

func (t *ListProjectsTool) Name() string {
	return "list_projects"
}

func (t *ListProjectsTool) Description() string {
	return `List all projects accessible in the current context. Returns each project's resource_name (the value you pass as 'project' to other tools) and its display_name. Call this first if you don't yet know which project to operate on.`
}

func (t *ListProjectsTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ListProjectsTool) Execute(ctx context.Context, execCtx ExecutionContext, _ map[string]interface{}) ToolResult {
	if execCtx.ProjectLister == nil {
		return ToolResult{Success: false, Error: "project lister not configured"}
	}

	projects, err := execCtx.ProjectLister.ListProjects(ctx)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("failed to list projects: %v", err)}
	}

	out := map[string]interface{}{
		"projects": projects,
		"total":    len(projects),
	}
	if len(projects) == 0 {
		out["hint"] = "No projects in scope. Create one in the cluster (TinyProject CRD) or via the platform UI before continuing."
	} else {
		out["hint"] = "Pass a project's resource_name as the 'project' argument to other tools (read_project, build_flow, etc.)."
	}
	return ToolResult{Success: true, Output: out}
}

var _ Tool = (*ListProjectsTool)(nil)
