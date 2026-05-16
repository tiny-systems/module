package tools

import (
	"context"
	"fmt"
)

// findNodeComponent looks up an existing node by ID and returns the
// ComponentInfo registered for its module/component. Used by edit_flow
// actions to enforce schema-strictness on configure_node and
// configure_edge — both need access to the target component's port
// schemas, which only the cluster knows.
//
// Returns nil ComponentInfo (and nil error) when:
//   - either ProjectReader or ModuleCatalog is unwired
//   - the node isn't in the project
//   - its module isn't in the catalog
//
// Callers treat "unknown component" as "skip the strictness check"
// rather than failing — a misconfigured execution context shouldn't
// block legitimate edits.
func findNodeComponent(ctx context.Context, execCtx ExecutionContext, nodeID string) (*ComponentInfo, error) {
	if execCtx.ProjectReader == nil || execCtx.ModuleCatalog == nil {
		return nil, nil
	}
	elements, err := execCtx.ProjectReader.ReadProjectElements(ctx, execCtx.ProjectName)
	if err != nil {
		return nil, fmt.Errorf("read project for node lookup: %w", err)
	}
	moduleName, componentName := findNodeIdentity(elements, nodeID)
	if moduleName == "" || componentName == "" {
		return nil, nil
	}
	mod, err := execCtx.ModuleCatalog.GetModule(ctx, moduleName)
	if err != nil || mod == nil {
		return nil, nil
	}
	for i := range mod.Components {
		if mod.Components[i].Name == componentName {
			return &mod.Components[i], nil
		}
	}
	return nil, nil
}

// findEdgeTarget walks the project elements for an edge with the given
// ID and returns the (targetNodeID, targetPort) pair. Used by
// configure_edge to resolve which component's port-schema to validate
// against — edge IDs alone don't carry that info.
func findEdgeTarget(ctx context.Context, execCtx ExecutionContext, edgeID string) (targetNodeID, targetPort string, err error) {
	if execCtx.ProjectReader == nil {
		return "", "", nil
	}
	elements, readErr := execCtx.ProjectReader.ReadProjectElements(ctx, execCtx.ProjectName)
	if readErr != nil {
		return "", "", fmt.Errorf("read project for edge lookup: %w", readErr)
	}
	for _, el := range elements.Elements {
		if id, _ := el["id"].(string); id != edgeID {
			continue
		}
		if t, _ := el["type"].(string); t != "tinyEdge" {
			continue
		}
		targetNodeID, _ = el["target"].(string)
		targetPort, _ = el["targetHandle"].(string)
		return targetNodeID, targetPort, nil
	}
	return "", "", nil
}

// findNodeIdentity locates a node element by ID and returns its
// module + component names. Returns empty strings when not found.
func findNodeIdentity(elements *ProjectElements, nodeID string) (moduleName, componentName string) {
	if elements == nil {
		return "", ""
	}
	for _, el := range elements.Elements {
		if id, _ := el["id"].(string); id != nodeID {
			continue
		}
		if t, _ := el["type"].(string); t != "tinyNode" {
			continue
		}
		data, _ := el["data"].(map[string]interface{})
		if data == nil {
			return "", ""
		}
		moduleName, _ = data["module"].(string)
		componentName, _ = data["component"].(string)
		return moduleName, componentName
	}
	return "", ""
}
