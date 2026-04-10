package tools

import (
	"context"
	"fmt"
	"time"
)

// GetTracesTool lists recent execution traces for a project
type GetTracesTool struct{}

func NewGetTracesTool() *GetTracesTool {
	return &GetTracesTool{}
}

func (t *GetTracesTool) Name() string {
	return "get_traces"
}

func (t *GetTracesTool) Description() string {
	return `List recent execution traces for the project. Shows whether the flow is running, how many spans executed, and if there are errors. Use this after building or modifying a flow to verify it works.

Returns traces sorted newest-first with: trace ID, span count, error count, data event count, and duration.`
}

func (t *GetTracesTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"flow": map[string]interface{}{
				"type":        "string",
				"description": "Filter traces to a specific flow (optional, defaults to all flows)",
			},
			"time_range": map[string]interface{}{
				"type":        "string",
				"description": "How far back to look for traces: 5m, 15m, 1h, 6h, 24h (default 1h)",
			},
			"errors_only": map[string]interface{}{
				"type":        "boolean",
				"description": "Only return traces that have errors (default false)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of traces to return (default 20, max 50)",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Skip this many traces for pagination (default 0)",
			},
		},
		"required": []string{},
	}
}

func (t *GetTracesTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ProjectName == "" {
		return ToolResult{
			Success: false,
			Error:   "project context is required",
		}
	}

	if execCtx.TraceReader == nil {
		return ToolResult{
			Success: false,
			Error:   "trace reader not configured",
		}
	}

	flowName, _ := input["flow"].(string)
	errorsOnly, _ := input["errors_only"].(bool)

	limit := 20
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 50 {
			limit = 50
		}
	}

	var offset int
	if o, ok := input["offset"].(float64); ok && o > 0 {
		offset = int(o)
	}

	lookback := time.Hour
	if tr, ok := input["time_range"].(string); ok && tr != "" {
		if parsed, err := parseDuration(tr); err == nil {
			lookback = parsed
		}
	}

	// Fetch more than limit if filtering, so we have enough after filtering
	fetchLimit := limit
	if errorsOnly {
		fetchLimit = limit * 5
		if fetchLimit > 200 {
			fetchLimit = 200
		}
	}

	traces, err := execCtx.TraceReader.ReadTraces(ctx, execCtx.ProjectName, flowName, lookback, offset, fetchLimit)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to get traces: %v", err),
		}
	}

	// Filter and summarize
	errorTraces := 0
	var filtered []TraceSummary
	for _, tr := range traces {
		if tr.Errors > 0 {
			errorTraces++
		}
		if errorsOnly && tr.Errors == 0 {
			continue
		}
		if len(filtered) < limit {
			filtered = append(filtered, tr)
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"project":      execCtx.ProjectName,
			"total":        len(filtered),
			"error_traces": errorTraces,
			"traces":       filtered,
		},
	}
}

// parseDuration parses human-friendly durations like "5m", "1h", "24h"
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

var _ Tool = (*GetTracesTool)(nil)
