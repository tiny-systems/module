package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// SendSignalTool sends a signal to a node's input port to trigger execution
type SendSignalTool struct{}

func NewSendSignalTool() *SendSignalTool {
	return &SendSignalTool{}
}

func (t *SendSignalTool) Name() string {
	return "send_signal"
}

func (t *SendSignalTool) Description() string {
	return `Send a signal to a node's input port to trigger execution. This creates a TinySignal CRD which the node's controller picks up and delivers as a message.

Use this to:
- Test a flow by triggering its entry point (e.g., signal or ticker node)
- Send specific test data to a node for debugging
- Kick off a flow after building it

Returns:
- trace_id: the signal arrival trace (1 span, usually). For trigger nodes that fan out (ticker, cron, signal), the actual execution chain is a separate trace.
- execution_traces: a list of recent traces discovered after the signal fired (within wait_ms). Each entry includes id, spans, errors, duration. Call get_trace_detail on the trace with the most spans to see the full chain.

The node_id is the full node identifier from read_project output (e.g., "tinysystems-common-module-v1.signal-abc12").
The port defaults to "signal" which is the standard trigger port.

Example:
  send_signal(node_id: "tinysystems-common-module-v1.signal-abc12", port: "signal", data: {"message": "hello"})
  → {trace_id: "<signal>", execution_traces: [{id: "<chain>", spans: 8, errors: 0, ...}]}
  get_trace_detail(trace_id: "<chain>")`
}

func (t *SendSignalTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"node_id": map[string]interface{}{
				"type":        "string",
				"description": "Full node ID from read_project (e.g., 'tinysystems-common-module-v1.signal-abc12')",
			},
			"port": map[string]interface{}{
				"type":        "string",
				"description": "Input port to send data to (default: 'signal')",
			},
			"data": map[string]interface{}{
				"type":        "object",
				"description": "JSON data to send as the signal payload (optional, defaults to empty object)",
			},
			"wait_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Milliseconds to wait after firing before polling for execution traces (default 3000, max 10000, set 0 to skip polling). Use lower values for fast paths, higher for cold-start chains.",
			},
		},
		"required": []string{"node_id"},
	}
}

func (t *SendSignalTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.SignalSender == nil {
		return ToolResult{
			Success: false,
			Error:   "signal sender not configured",
		}
	}

	nodeID, _ := input["node_id"].(string)
	if nodeID == "" {
		return ToolResult{
			Success: false,
			Error:   "node_id is required. Use read_project to list nodes in the current flow and find the trigger node.",
		}
	}

	portName, _ := input["port"].(string)
	if portName == "" {
		portName = "signal"
	}

	// Marshal data to JSON bytes
	var data []byte
	if d, ok := input["data"]; ok && d != nil {
		var err error
		data, err = json.Marshal(d)
		if err != nil {
			return ToolResult{
				Success: false,
				Error:   fmt.Sprintf("failed to marshal data: %v", err),
			}
		}
	}

	// Generate a trace ID so the model can look up execution results
	traceID, err := generateTraceID()
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to generate trace ID: %v", err),
		}
	}

	signalSentAt := time.Now()
	if err := execCtx.SignalSender.SendSignal(ctx, execCtx.ProjectName, nodeID, portName, data, traceID); err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to send signal: %v", err),
		}
	}

	waitMs := 3000
	if w, ok := input["wait_ms"]; ok {
		switch v := w.(type) {
		case float64:
			waitMs = int(v)
		case int:
			waitMs = v
		}
	}
	if waitMs < 0 {
		waitMs = 0
	}
	if waitMs > 10000 {
		waitMs = 10000
	}

	executionTraces := pollExecutionTraces(ctx, execCtx, traceID, signalSentAt, waitMs)

	status := "signal sent. If execution_traces is empty, the chain may not have fired yet — call get_traces after a few seconds."
	if len(executionTraces) > 0 {
		status = fmt.Sprintf("signal sent. Found %d execution trace(s) — call get_trace_detail on the one with the most spans to inspect the chain.", len(executionTraces))
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"node_id":          nodeID,
			"port":             portName,
			"trace_id":         traceID,
			"execution_traces": executionTraces,
			"status":           status,
		},
	}
}

// pollExecutionTraces waits briefly then queries the trace reader for
// traces that started after the signal fired. Returns up to 10 summaries
// sorted by span count (likely-chain first). The signal's own trace is
// excluded.
func pollExecutionTraces(ctx context.Context, execCtx ExecutionContext, signalTraceID string, signalSentAt time.Time, waitMs int) []map[string]interface{} {
	empty := []map[string]interface{}{}
	if waitMs == 0 || execCtx.TraceReader == nil || execCtx.ProjectName == "" {
		return empty
	}
	select {
	case <-time.After(time.Duration(waitMs) * time.Millisecond):
	case <-ctx.Done():
		return empty
	}

	traces, err := execCtx.TraceReader.ReadTraces(ctx, execCtx.ProjectName, "", 30*time.Second, 0, 50)
	if err != nil {
		return empty
	}

	signalMicros := signalSentAt.UnixMicro()
	out := make([]map[string]interface{}, 0, len(traces))
	for _, t := range traces {
		// Two trace shapes are possible:
		//  - _control signal: chain runs under a NEW trace_id, signal
		//    arrival is its own single-span trace under signalTraceID.
		//    Skip the signal trace and surface the new chain.
		//  - regular input port signal: chain inherits the signal's
		//    trace_id (multi-span trace under signalTraceID). Keep it.
		if t.ID == signalTraceID && t.Spans <= 1 {
			continue
		}
		if t.Start < signalMicros-100_000 { // 100ms grace for clock skew
			continue
		}
		out = append(out, map[string]interface{}{
			"id":          t.ID,
			"spans":       t.Spans,
			"errors":      t.Errors,
			"data":        t.Data,
			"duration_ns": t.Duration,
		})
		if len(out) >= 10 {
			break
		}
	}
	return out
}


// generateTraceID creates a random 16-byte trace ID as a 32-char hex string
func generateTraceID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var _ Tool = (*SendSignalTool)(nil)
