package tools

import (
	"context"
	"fmt"
	"strings"
)

// CloneSolutionTool clones a solution's flow into the current flow.
// It loads solution data and recreates nodes, settings, edges, and edge configs.
type CloneSolutionTool struct{}

func NewCloneSolutionTool() *CloneSolutionTool {
	return &CloneSolutionTool{}
}

func (t *CloneSolutionTool) Name() string {
	return "clone_solution"
}

func (t *CloneSolutionTool) Description() string {
	return `Clone a solution into the current flow, recreating all nodes, edges, and configurations.

Use search_solutions to find solutions, then clone_solution to replicate one in the current flow.

Input:
- solution_uuid: UUID from search_solutions or get_solution
- flow_index: (optional) Index of the flow to clone if solution has multiple flows (default: 0)

This tool:
1. Loads the solution via get_solution
2. Creates all nodes from the solution flow
3. Applies node settings (where present)
4. Creates all edges with proper ID remapping
5. Applies edge configurations

Returns created node/edge IDs and any errors. Partial results are returned on failures.
Settings schemas are NOT preserved — use edit_flow with action=configure_node to add schemas for configurable fields after cloning.`
}

func (t *CloneSolutionTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"solution_uuid": map[string]interface{}{
				"type":        "string",
				"description": "Solution UUID from search_solutions",
			},
			"flow_index": map[string]interface{}{
				"type":        "integer",
				"description": "Index of the flow to clone (default: 0, first flow)",
			},
		},
		"required": []string{"solution_uuid"},
	}
}

func (t *CloneSolutionTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	// Validate required interfaces
	if execCtx.SolutionSearcher == nil {
		return ToolResult{Success: false, Error: "solution search not configured"}
	}
	if execCtx.NodeAdder == nil {
		return ToolResult{Success: false, Error: "node adder not configured"}
	}
	if execCtx.EdgeAdder == nil {
		return ToolResult{Success: false, Error: "edge adder not configured"}
	}

	uuid, _ := input["solution_uuid"].(string)
	if uuid == "" {
		return ToolResult{Success: false, Error: "solution_uuid is required"}
	}

	flowIndex := 0
	if fi, ok := input["flow_index"].(float64); ok {
		flowIndex = int(fi)
	}

	// Load solution
	solution, err := execCtx.SolutionSearcher.GetSolution(ctx, uuid)
	if err != nil {
		return ToolResult{Success: false, Error: "failed to load solution: " + err.Error()}
	}
	if solution == nil {
		return ToolResult{Success: false, Error: "solution not found"}
	}
	if len(solution.Flows) == 0 {
		return ToolResult{Success: false, Error: "solution has no flows"}
	}
	if flowIndex < 0 || flowIndex >= len(solution.Flows) {
		return ToolResult{Success: false, Error: fmt.Sprintf("flow_index %d out of range (solution has %d flows)", flowIndex, len(solution.Flows))}
	}

	flow := solution.Flows[flowIndex]

	if len(flow.Nodes) == 0 {
		return ToolResult{Success: false, Error: "solution flow has no nodes"}
	}

	// Create nodes, mapping old IDs to new IDs
	oldToNewID := make(map[string]string)
	var nodesCreated []map[string]interface{}
	var errors []string
	var warnings []string

	for _, node := range flow.Nodes {
		if ctx.Err() != nil {
			errors = append(errors, fmt.Sprintf("cancelled during node creation (%s)", node.Component))
			break
		}

		result, err := execCtx.NodeAdder.AddNode(ctx, execCtx.ProjectName, execCtx.FlowName, node.Component, node.Module, execCtx.PositionTracker)
		if err != nil {
			errors = append(errors, fmt.Sprintf("node '%s' (%s): %s", node.ID, node.Component, err.Error()))
			continue
		}

		oldToNewID[node.ID] = result.NodeID
		nodesCreated = append(nodesCreated, map[string]interface{}{
			"old_id":    node.ID,
			"node_id":   result.NodeID,
			"component": node.Component,
			"module":    node.Module,
			"ports":     result.Ports,
		})
	}

	// Apply node settings (before edges — settings may create ports)
	for _, node := range flow.Nodes {
		if node.Settings == nil {
			continue
		}
		newID, ok := oldToNewID[node.ID]
		if !ok {
			continue // node failed to create
		}
		if ctx.Err() != nil {
			errors = append(errors, fmt.Sprintf("cancelled during settings (%s)", node.Component))
			break
		}
		if execCtx.NodeSettingsConfigurer == nil {
			warnings = append(warnings, fmt.Sprintf("node '%s': settings configurer not available, skipping settings", node.Component))
			continue
		}

		// Note: settings_schema is not available in solution data, pass nil
		result, err := execCtx.NodeSettingsConfigurer.ConfigureNodeSettings(ctx, execCtx.ProjectName, execCtx.FlowName, newID, node.Settings, nil)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("node '%s' settings: %s", node.Component, err.Error()))
			continue
		}
		if !result.Valid {
			msg := fmt.Sprintf("node '%s' settings rejected: %s", node.Component, result.Error)
			if result.Hint != "" {
				msg += " (hint: " + result.Hint + ")"
			}
			warnings = append(warnings, msg)
		}
		// Update ports if settings changed them
		if len(result.Ports) > 0 {
			for i, nc := range nodesCreated {
				if nc["old_id"] == node.ID {
					nodesCreated[i]["ports"] = result.Ports
					nodesCreated[i]["settings_applied"] = true
					break
				}
			}
		}
	}

	// Create edges with ID remapping
	var edgesOutput []map[string]interface{}
	type createdEdge struct {
		EdgeID string
		Index  int // index in flow.Edges for config step
	}
	var edgesCreated []createdEdge

	for i, edge := range flow.Edges {
		fromNodeID, fromOK := oldToNewID[edge.Source]
		toNodeID, toOK := oldToNewID[edge.Target]

		if !fromOK || !toOK {
			var missing []string
			if !fromOK {
				missing = append(missing, fmt.Sprintf("source '%s'", edge.Source))
			}
			if !toOK {
				missing = append(missing, fmt.Sprintf("target '%s'", edge.Target))
			}
			errors = append(errors, fmt.Sprintf("edge %s:%s -> %s:%s: skipped, node not created: %s",
				edge.Source, edge.SourceHandle, edge.Target, edge.TargetHandle, strings.Join(missing, ", ")))
			continue
		}
		if ctx.Err() != nil {
			errors = append(errors, fmt.Sprintf("cancelled during edge creation (%s -> %s)", edge.Source, edge.Target))
			break
		}

		result, err := execCtx.EdgeAdder.AddEdge(ctx, execCtx.ProjectName, execCtx.FlowName, fromNodeID, edge.SourceHandle, toNodeID, edge.TargetHandle)
		if err != nil {
			errors = append(errors, fmt.Sprintf("edge %s:%s -> %s:%s: %s",
				edge.Source, edge.SourceHandle, edge.Target, edge.TargetHandle, err.Error()))
			continue
		}

		edgesCreated = append(edgesCreated, createdEdge{
			EdgeID: result.EdgeID,
			Index:  i,
		})
		edgesOutput = append(edgesOutput, map[string]interface{}{
			"from":    edge.Source + ":" + edge.SourceHandle,
			"to":      edge.Target + ":" + edge.TargetHandle,
			"edge_id": result.EdgeID,
		})
	}

	// Configure edges
	for j, ce := range edgesCreated {
		edge := flow.Edges[ce.Index]
		if edge.Configuration == nil {
			continue
		}
		if ctx.Err() != nil {
			errors = append(errors, fmt.Sprintf("cancelled during edge config"))
			break
		}
		if execCtx.EdgeConfigurer == nil {
			warnings = append(warnings, "edge configurer not available, skipping edge configurations")
			break
		}

		result, err := execCtx.EdgeConfigurer.ConfigureEdge(ctx, execCtx.ProjectName, execCtx.FlowName, ce.EdgeID, edge.Configuration, nil, "")
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("edge %s:%s -> %s:%s config: %s",
				edge.Source, edge.SourceHandle, edge.Target, edge.TargetHandle, err.Error()))
			edgesOutput[j]["config_valid"] = false
			continue
		}

		edgesOutput[j]["config_valid"] = result.Valid
		if !result.Valid {
			msg := fmt.Sprintf("edge %s:%s -> %s:%s validation: %s",
				edge.Source, edge.SourceHandle, edge.Target, edge.TargetHandle, result.Error)
			if result.Hint != "" {
				msg += " (hint: " + result.Hint + ")"
			}
			warnings = append(warnings, msg)
		}
	}

	// Build output
	output := map[string]interface{}{
		"solution":      solution.Title,
		"flow":          flow.Title,
		"nodes_created": nodesCreated,
		"edges_created": edgesOutput,
	}
	if len(errors) > 0 {
		output["errors"] = errors
	}
	if len(warnings) > 0 {
		output["warnings"] = warnings
	}

	success := len(nodesCreated) > 0
	if !success {
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("no nodes were created. Errors: %s", strings.Join(errors, "; ")),
			Output:  output,
		}
	}

	return ToolResult{
		Success: true,
		Output:  output,
	}
}

var _ Tool = (*CloneSolutionTool)(nil)
