package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

Returns a trace_id that can be used with get_trace_detail to inspect the execution result.

The node_id is the full node identifier from read_project output (e.g., "tinysystems-common-module-v1.signal-abc12").
The port defaults to "signal" which is the standard trigger port.

Example:
  send_signal(node_id: "tinysystems-common-module-v1.signal-abc12", port: "signal", data: {"message": "hello"})
  → {trace_id: "abc123...", ...}
  get_trace_detail(trace_id: "abc123...")`
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

	if err := execCtx.SignalSender.SendSignal(ctx, execCtx.ProjectName, nodeID, portName, data, traceID); err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to send signal: %v", err),
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"node_id":   nodeID,
			"port": portName,
			"trace_id":  traceID,
			"status":    "signal sent — use get_trace_detail(trace_id) to inspect execution results",
		},
	}
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
