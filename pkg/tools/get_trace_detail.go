package tools

import (
	"context"
	"fmt"
)

// GetTraceDetailTool gets full span details for a specific trace
type GetTraceDetailTool struct{}

func NewGetTraceDetailTool() *GetTraceDetailTool {
	return &GetTraceDetailTool{}
}

func (t *GetTraceDetailTool) Name() string {
	return "get_trace_detail"
}

func (t *GetTraceDetailTool) Description() string {
	return `Get detailed span information for a specific trace. Shows the execution path through nodes, data passed between them, and any errors. Use this to debug why a flow failed or to verify data transformations.

Each span shows: source node, destination node, port, duration, status, and events (data payloads and errors).`
}

func (t *GetTraceDetailTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"trace_id": map[string]interface{}{
				"type":        "string",
				"description": "The trace ID from get_traces results",
			},
		},
		"required": []string{"trace_id"},
	}
}

func (t *GetTraceDetailTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
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

	traceID, _ := input["trace_id"].(string)
	if traceID == "" {
		return ToolResult{
			Success: false,
			Error:   "trace_id is required",
		}
	}

	spans, err := execCtx.TraceReader.ReadTraceDetail(ctx, execCtx.ProjectName, traceID)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to get trace detail: %v", err),
		}
	}

	errorCount := 0
	for _, s := range spans {
		for _, e := range s.Events {
			if e.Name == "error" || e.Name == "exception" {
				errorCount++
			}
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"trace_id":    traceID,
			"total_spans": len(spans),
			"errors":      errorCount,
			"spans":       spans,
		},
	}
}

var _ Tool = (*GetTraceDetailTool)(nil)
