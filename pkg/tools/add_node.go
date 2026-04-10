package tools

import (
	"context"
	"fmt"
)

// AddNodeTool adds a node to the flow using semantic parameters
type AddNodeTool struct{}

func NewAddNodeTool() *AddNodeTool {
	return &AddNodeTool{}
}

func (t *AddNodeTool) Name() string {
	return "add_node"
}

func (t *AddNodeTool) Description() string {
	return `Add a new node to the current flow.

IMPORTANT: Component names must be EXACT matches from list_modules output.
Do NOT guess component names - call list_modules first to see available components.
Common components: server, client (http-module), signal, router, modify (common-module).

Returns the node_id and list of available ports for connecting edges.

Example:
  add_node(component: "router", module: "tinysystems/common-module-v1")
  → {node_id: "router-abc123", ports: ["input", "match", "default"]}`
}

func (t *AddNodeTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"component": map[string]interface{}{
				"type":        "string",
				"description": "Exact component name from list_modules (e.g., 'router', 'server'). Do NOT guess - use list_modules first.",
			},
			"module": map[string]interface{}{
				"type":        "string",
				"description": "Module name from list_modules (e.g., 'workspace/common-module-v1')",
			},
		},
		"required": []string{"component", "module"},
	}
}

func (t *AddNodeTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.NodeAdder == nil {
		return ToolResult{
			Success: false,
			Error:   "node adder not configured",
		}
	}

	component, _ := input["component"].(string)
	module, _ := input["module"].(string)

	if component == "" {
		return ToolResult{
			Success: false,
			Error:   "component is required",
		}
	}

	if module == "" {
		return ToolResult{
			Success: false,
			Error:   "module is required",
		}
	}

	// Validate component exists in module using ModuleCatalog
	// This validation is done in the tool layer so it's testable
	if execCtx.ModuleCatalog != nil {
		moduleInfo, err := execCtx.ModuleCatalog.GetModule(ctx, module)
		if err != nil {
			return ToolResult{
				Success: false,
				Error:   fmt.Sprintf("failed to lookup module: %s", err.Error()),
			}
		}
		if moduleInfo == nil {
			return ToolResult{
				Success: false,
				Error:   fmt.Sprintf("module '%s' not found. Use list_modules to see available modules.", module),
			}
		}

		// Check if component exists in module
		var availableComponents []string
		componentFound := false
		for _, c := range moduleInfo.Components {
			availableComponents = append(availableComponents, c.Name)
			if c.Name == component {
				componentFound = true
				break
			}
		}
		if !componentFound {
			return ToolResult{
				Success: false,
				Error:   fmt.Sprintf("component '%s' not found in module '%s'. Available components: %v", component, module, availableComponents),
			}
		}
	}

	result, err := execCtx.NodeAdder.AddNode(ctx, execCtx.ProjectName, execCtx.FlowName, component, module, execCtx.PositionTracker)
	if err != nil {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to add node: %s", err.Error()),
		}
	}

	return ToolResult{
		Success: true,
		Output: map[string]interface{}{
			"node_id": result.NodeID,
			"ports":   result.Ports,
			"hint":    "Use add_edge to connect this node to other nodes",
		},
	}
}

var _ Tool = (*AddNodeTool)(nil)
