package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ConfigureEdgeTool sets the data mapping for an edge
type ConfigureEdgeTool struct{}

func NewConfigureEdgeTool() *ConfigureEdgeTool {
	return &ConfigureEdgeTool{}
}

func (t *ConfigureEdgeTool) Name() string {
	return "configure_edge"
}

func (t *ConfigureEdgeTool) Description() string {
	return `Configure how data maps from source port to target port.

Use {{expression}} syntax for dynamic values:
- Literal values: "hello", 42, true, {"key": "value"}
- Expressions: "{{$.field}}", "{{$.nested.path}}"
- Interpolation: "Hello {{$.name}}!"
- Pure expressions return actual type: "{{$.data}}" → object/array/number

IMPORTANT: Before configuring, use get_node_port_schema on the SOURCE port to understand what fields are available ($.fieldname).

If validation fails, READ THE HINT - it shows available fields at the source port (e.g., "Available fields at source: 'eventType', 'name', 'namespace'"). Use this to correct your field paths without needing additional tool calls.

If trace_id is provided, validates against real execution data.

## Extending Configurable Schemas on Edges

When the target port has configurable fields (marked with configurable:true), provide a "schema"
parameter to define their type structure. Without schema, configurable fields are empty {} and
downstream validation fails.

Use edge schema extension for components like template engines where input shape is dynamic:
  configure_edge(edge_id: "edge-xyz", configuration: {
    "template": "<h1>{{.city}}</h1>",
    "renderData": {
      "city": "{{$.city}}",
      "country": "{{$.country}}"
    }
  }, schema: {
    "renderData": {
      "type": "object",
      "properties": {
        "city": {"type": "string", "title": "City"},
        "country": {"type": "string", "title": "Country"}
      }
    }
  })

## Context Passthrough Pattern - CRITICAL

Pass through only the ORIGINAL context at each hop using context: "{{$.context}}" (NOT "{{$}}").
This keeps paths flat - {{$.context.fieldName}} works at ANY depth:

  BAD:  context: "{{$}}"           → nests everything, paths grow: $.context.context.context.field
  GOOD: context: "{{$.context}}"   → passes through original, paths stay flat: $.context.field

Example - wiring from a node that received Ticker context:
  configure_edge(edge_id: "edge-xyz", configuration: {
    "slackToken": "{{$.context.slackToken}}",   // flat path - works at any depth!
    "eventData": "{{$.eventType}}",              // current node's data at top level
    "context": "{{$.context}}"                   // pass through original context
  })

Simple example without schema:
  configure_edge(edge_id: "edge-xyz", configuration: {
    "message": "{{$.body}}",
    "timestamp": "{{now()}}",
    "status": 200
  })`
}

func (t *ConfigureEdgeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"edge_id": map[string]interface{}{
				"type":        "string",
				"description": "The edge ID to configure",
			},
			"configuration": map[string]interface{}{
				"description": "The data mapping configuration using {{expression}} syntax for dynamic values. Can be a JSON object or a JSON string that parses to an object.",
			},
			"schema": map[string]interface{}{
				"type":        "object",
				"description": "Optional schema extensions for configurable fields on the target port. Keys are field names (e.g., 'renderData'), values are JSON Schema definitions that extend/override the field's schema.",
			},
			"trace_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional trace ID to validate against real execution data",
			},
		},
		"required": []string{"edge_id", "configuration"},
	}
}

func (t *ConfigureEdgeTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.EdgeConfigurer == nil {
		return ToolResult{
			Success: false,
			Error:   "edge configurer not configured",
		}
	}

	edgeID, _ := input["edge_id"].(string)
	schema, _ := input["schema"].(map[string]interface{})
	traceID, _ := input["trace_id"].(string)

	if edgeID == "" {
		return ToolResult{
			Success: false,
			Error:   "edge_id is required",
		}
	}

	config, configOk := input["configuration"].(map[string]interface{})
	if !configOk {
		// Try parsing string as JSON object
		if configStr, isString := input["configuration"].(string); isString {
			if err := json.Unmarshal([]byte(configStr), &config); err != nil {
				return ToolResult{
					Success: false,
					Error:   fmt.Sprintf("configuration string is not valid JSON: %v", err),
				}
			}
		} else {
			return ToolResult{
				Success: false,
				Error:   "configuration is required and must be a JSON object or a JSON string",
			}
		}
	}

	result, err := execCtx.EdgeConfigurer.ConfigureEdge(ctx, execCtx.ProjectName, execCtx.FlowName, edgeID, config, schema, traceID)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to configure edge: %s", err.Error()),
		}
	}

	if !result.Valid {
		output := map[string]interface{}{
			"valid": false,
			"error": result.Error,
		}
		if result.Hint != "" {
			output["hint"] = result.Hint
		}
		return ToolResult{
			Success: false,
			Error:   result.Error,
			Output:  output,
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"valid":   true,
			"edge_id": edgeID,
		},
	}
}

var _ Tool = (*ConfigureEdgeTool)(nil)
