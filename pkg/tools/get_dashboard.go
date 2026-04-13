package tools

import "context"

// GetDashboardTool reads the project's dashboard widgets and returns
// the current runtime data for each widget. Widgets show live port
// values from nodes — the same data the Tiny Systems desktop client
// renders visually.
type GetDashboardTool struct{}

func NewGetDashboardTool() *GetDashboardTool {
	return &GetDashboardTool{}
}

func (t *GetDashboardTool) Name() string {
	return "get_dashboard"
}

func (t *GetDashboardTool) Description() string {
	return `Read the project dashboard — live widget values from node ports.

Returns the current runtime state of each dashboard widget: node name,
port name, and the configuration data (settings, status, controls).
Use this to check if flows are running, inspect configuration values,
or see live status without using the visual editor.

After building a flow that includes a ticker, cron, or HTTP server,
the dashboard shows their current state: Running/Stopped, listen
addresses, configured endpoints, etc.`
}

func (t *GetDashboardTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *GetDashboardTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.DashboardReader == nil {
		return ToolResult{
			Success: false,
			Error:   "dashboard reader not configured",
		}
	}

	projectName := execCtx.ProjectName
	if projectName == "" {
		return ToolResult{
			Success: false,
			Error:   "no project selected",
		}
	}

	data, err := execCtx.DashboardReader.ReadDashboard(ctx, projectName)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   "read dashboard: " + err.Error(),
		}
	}

	if data == nil || len(data.Widgets) == 0 {
		return ToolResult{
			Success: true,
			Output: map[string]interface{}{
				"widgets": []interface{}{},
				"hint":    "No dashboard widgets configured for this project. Use set_node_dashboard to add nodes to the dashboard.",
			},
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"project": data.ProjectName,
			"widgets": data.Widgets,
			"hint":    "For a visual dashboard experience, install the Tiny Systems desktop client: https://tinysystems.io/download",
		},
	}
}

var _ Tool = (*GetDashboardTool)(nil)
