package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Registry manages the available LLM tools.
//
// The registry is backend-agnostic: it holds tools that conform to the
// Tool interface and executes them with an ExecutionContext provided by
// the caller. Provider-specific adapters (Anthropic, OpenAI, MCP protocol)
// should wrap the registry at a higher layer and convert Tool.Schema() into
// their own tool-definition format.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates a new tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// List returns all registered tools
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		result = append(result, tool)
	}
	return result
}

// Execute runs a tool by name with the given input
func (r *Registry) Execute(ctx context.Context, execCtx ExecutionContext, name string, input map[string]interface{}) ToolResult {
	tool, ok := r.Get(name)
	if !ok {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("unknown tool: %s", name),
		}
	}
	return tool.Execute(ctx, execCtx, input)
}

// ExecuteAndFormat runs a tool and returns the result as a JSON string.
// On error, the returned string is a JSON object with "error" and optionally "output" keys.
// The second return value is true if the tool succeeded.
func (r *Registry) ExecuteAndFormat(ctx context.Context, execCtx ExecutionContext, name string, input map[string]interface{}) (string, bool) {
	result := r.Execute(ctx, execCtx, name, input)

	if !result.Success {
		if result.Output != nil {
			errorWithOutput := map[string]interface{}{
				"error":  result.Error,
				"output": result.Output,
			}
			output, err := json.Marshal(errorWithOutput)
			if err == nil {
				return string(output), false
			}
		}
		return fmt.Sprintf(`{"error": %q}`, result.Error), false
	}

	output, err := json.Marshal(result.Output)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to marshal output: %s"}`, err.Error()), false
	}

	return string(output), true
}
