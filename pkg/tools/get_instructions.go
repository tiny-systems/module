package tools

import (
	"context"
)

// GetInstructionsTool returns the Tiny Systems platform guide so external
// AI clients (MCP) can learn how to use the tools without manual explanation.
type GetInstructionsTool struct {
	instructions string
}

func NewGetInstructionsTool(instructions string) *GetInstructionsTool {
	return &GetInstructionsTool{instructions: instructions}
}

func (t *GetInstructionsTool) Name() string {
	return "get_instructions"
}

func (t *GetInstructionsTool) Description() string {
	return `Get the Tiny Systems platform guide — how to build flows, use tools, expression syntax, signals, schema extension, and key rules. Call this FIRST before using any other tool if you are unfamiliar with Tiny Systems.`
}

func (t *GetInstructionsTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *GetInstructionsTool) Execute(_ context.Context, _ ExecutionContext, _ map[string]interface{}) ToolResult {
	return ToolResult{
		Success: true,
		Output:  t.instructions,
	}
}

var _ Tool = (*GetInstructionsTool)(nil)
