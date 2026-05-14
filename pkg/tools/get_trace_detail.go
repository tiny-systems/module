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
	var issues []map[string]interface{}
	for _, s := range spans {
		for _, e := range s.Events {
			switch e.Name {
			case "error", "exception":
				errorCount++
				issues = append(issues, map[string]interface{}{
					"kind":    "error",
					"span_id": s.SpanID,
					"from":    s.From,
					"to":      s.To,
					"port":    s.Port,
					"detail":  e.Data,
				})
			case "expression_error":
				// Expression failures don't always abort the chain (router
				// treats them as false). Surface them as issues so the
				// model can see "expression X failed" without scanning
				// every span's events.
				issues = append(issues, map[string]interface{}{
					"kind":       "expression_error",
					"span_id":    s.SpanID,
					"from":       s.From,
					"to":         s.To,
					"port":       s.Port,
					"expression": e.Data["expression"],
					"error":      e.Data["error"],
				})
			}
		}
	}

	output := map[string]interface{}{
		"trace_id":    traceID,
		"total_spans": len(spans),
		"errors":      errorCount,
		"spans":       spans,
	}
	if len(issues) > 0 {
		output["issues"] = issues
	}

	return ToolResult{
		Success: true,
		Output:  output,
	}
}

var _ Tool = (*GetTraceDetailTool)(nil)
