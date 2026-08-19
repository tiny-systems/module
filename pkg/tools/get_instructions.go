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
	return `Get the Tiny Systems platform guide — how to build flows, use tools, expression syntax, signals, schema extension, and key rules. Call this FIRST before using any other tool if you are unfamiliar with Tiny Systems.

Returns what every flow needs, and ends with an index of sections held back because most flows never need them (dashboards, forms, agent loops, code components, scenarios, endpoint checks, publishing). Pass section="<key>" to read one — do that BEFORE building that part rather than improvising it.`
}

func (t *GetInstructionsTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"section": map[string]interface{}{
				"type":        "string",
				"description": "Read one held-back section instead of the core guide. Keys are listed at the end of the core guide.",
			},
		},
	}
}

func (t *GetInstructionsTool) Execute(_ context.Context, _ ExecutionContext, input map[string]interface{}) ToolResult {
	if section, _ := input["section"].(string); section != "" {
		body, err := sectionPrompt(t.instructions, section)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}
		}
		return ToolResult{
			Success: true,
			Output: map[string]interface{}{
				"section":      section,
				"instructions": body,
			},
		}
	}
	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"instructions": corePrompt(t.instructions),
		},
	}
}

var _ Tool = (*GetInstructionsTool)(nil)
