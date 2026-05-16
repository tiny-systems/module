package tools

import (
	"context"
	"strings"
	"testing"
)

// projectReaderStub returns a fixed ProjectElements payload so
// findNodeComponent / findEdgeTarget have something to walk.
type projectReaderStub struct {
	elements *ProjectElements
	err      error
}

func (s *projectReaderStub) ReadProjectElements(_ context.Context, _ string) (*ProjectElements, error) {
	return s.elements, s.err
}

// projectWithNode returns ProjectElements describing a single node
// `nodeID` belonging to component `componentName` in module
// `moduleName`.
func projectWithNode(nodeID, moduleName, componentName string) *ProjectElements {
	return &ProjectElements{
		Elements: []map[string]interface{}{
			{
				"id":   nodeID,
				"type": "tinyNode",
				"data": map[string]interface{}{
					"module":    moduleName,
					"component": componentName,
				},
			},
		},
	}
}

// projectWithEdge adds an edge element targeting (targetNodeID, targetPort)
// to a project that also carries the target node so findNodeComponent
// resolves.
func projectWithEdge(edgeID, targetNodeID, targetPort, moduleName, componentName string) *ProjectElements {
	return &ProjectElements{
		Elements: []map[string]interface{}{
			{
				"id":   targetNodeID,
				"type": "tinyNode",
				"data": map[string]interface{}{
					"module":    moduleName,
					"component": componentName,
				},
			},
			{
				"id":           edgeID,
				"type":         "tinyEdge",
				"target":       targetNodeID,
				"targetHandle": targetPort,
			},
		},
	}
}

// TestEditFlow_ConfigureNode_RejectsMissingSchemaForConfigurableField
// pins the per-edit strict check: omitting schema for a configurable
// settings field fails before NodeSettingsConfigurer is called.
func TestEditFlow_ConfigureNode_RejectsMissingSchemaForConfigurableField(t *testing.T) {
	cfg := &captureNodeSettingsConfigurer{}
	reader := &projectReaderStub{elements: projectWithNode("node-1", "tinysystems/test-module", "ticker")}
	catalog := &mockModuleCatalog{components: map[string]ComponentInfo{
		"ticker": {
			Name:        "ticker",
			InputPorts:  []string{"_settings"},
			OutputPorts: []string{"out"},
			InputPortDetails: []PortDetail{
				{Name: "_settings", Schema: settingsSchemaWithConfigurableContext()},
			},
		},
	}}
	tool := NewEditFlowTool()

	res := tool.Execute(context.Background(), ExecutionContext{
		ProjectName:            "p1",
		FlowName:               "f1",
		ProjectReader:          reader,
		ModuleCatalog:          catalog,
		NodeSettingsConfigurer: cfg,
	}, map[string]interface{}{
		"action":  "configure_node",
		"node_id": "node-1",
		"settings": map[string]interface{}{
			"context": map[string]interface{}{"token": "secret"},
		},
		// no "schema" — strictness MUST reject
	})

	if res.Success {
		t.Fatalf("expected rejection, got success")
	}
	if !strings.Contains(res.Error, "context") {
		t.Errorf("error should name the missing field 'context'; got: %s", res.Error)
	}
	if cfg.gotSettings != nil {
		t.Error("rejected edit must not call NodeSettingsConfigurer")
	}
}

// TestEditFlow_ConfigureNode_AcceptsExplicitSchema is the happy-path
// mirror: providing schema for the configurable field lets the call
// through.
func TestEditFlow_ConfigureNode_AcceptsExplicitSchema(t *testing.T) {
	cfg := &captureNodeSettingsConfigurer{}
	reader := &projectReaderStub{elements: projectWithNode("node-1", "tinysystems/test-module", "ticker")}
	catalog := &mockModuleCatalog{components: map[string]ComponentInfo{
		"ticker": {
			Name:        "ticker",
			InputPorts:  []string{"_settings"},
			OutputPorts: []string{"out"},
			InputPortDetails: []PortDetail{
				{Name: "_settings", Schema: settingsSchemaWithConfigurableContext()},
			},
		},
	}}
	tool := NewEditFlowTool()

	res := tool.Execute(context.Background(), ExecutionContext{
		ProjectName:            "p1",
		FlowName:               "f1",
		ProjectReader:          reader,
		ModuleCatalog:          catalog,
		NodeSettingsConfigurer: cfg,
	}, map[string]interface{}{
		"action":  "configure_node",
		"node_id": "node-1",
		"settings": map[string]interface{}{
			"context": map[string]interface{}{"token": "secret"},
		},
		"schema": map[string]interface{}{
			"context": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"token": map[string]interface{}{"type": "string"}},
			},
		},
	})

	if !res.Success {
		t.Fatalf("expected success, got: %s", res.Error)
	}
	if cfg.gotSettings == nil {
		t.Error("expected NodeSettingsConfigurer to receive the settings")
	}
}

// TestEditFlow_ConfigureNode_SkipsCheckWhenContextUnreachable proves
// the strict check is best-effort: missing ProjectReader / ModuleCatalog
// does not block legitimate edits. (Used in test rigs that don't wire
// every adapter.)
func TestEditFlow_ConfigureNode_SkipsCheckWhenContextUnreachable(t *testing.T) {
	cfg := &captureNodeSettingsConfigurer{}
	tool := NewEditFlowTool()

	res := tool.Execute(context.Background(), ExecutionContext{
		ProjectName:            "p1",
		FlowName:               "f1",
		NodeSettingsConfigurer: cfg,
		// no ProjectReader / ModuleCatalog
	}, map[string]interface{}{
		"action":  "configure_node",
		"node_id": "node-1",
		"settings": map[string]interface{}{
			"context": map[string]interface{}{"token": "secret"},
		},
	})

	if !res.Success {
		t.Fatalf("expected success when catalog unreachable, got: %s", res.Error)
	}
}

// TestEditFlow_ConfigureEdge_RejectsMissingSchemaForConfigurableTargetField
// is the edge-side mirror: filling a configurable target field
// without an `edge.schema` entry must reject.
func TestEditFlow_ConfigureEdge_RejectsMissingSchemaForConfigurableTargetField(t *testing.T) {
	cfg := &captureEdgeConfigurer{}
	reader := &projectReaderStub{
		elements: projectWithEdge("edge-1", "node-target", "request", "tinysystems/test-module", "receiver"),
	}
	catalog := &mockModuleCatalog{components: map[string]ComponentInfo{
		"receiver": {
			Name:        "receiver",
			InputPorts:  []string{"_settings", "request"},
			OutputPorts: []string{"response"},
			InputPortDetails: []PortDetail{
				{Name: "request", Schema: targetPortSchemaWithConfigurableContext()},
			},
		},
	}}
	tool := NewEditFlowTool()

	res := tool.Execute(context.Background(), ExecutionContext{
		ProjectName:    "p1",
		FlowName:       "f1",
		ProjectReader:  reader,
		ModuleCatalog:  catalog,
		EdgeConfigurer: cfg,
	}, map[string]interface{}{
		"action":  "configure_edge",
		"edge_id": "edge-1",
		"configuration": map[string]interface{}{
			"context": map[string]interface{}{"apiKey": "k1"},
		},
		// no schema — strictness MUST reject
	})

	if res.Success {
		t.Fatalf("expected rejection, got success")
	}
	if !strings.Contains(res.Error, "context") {
		t.Errorf("error should name the missing field 'context'; got: %s", res.Error)
	}
	if cfg.gotConfig != nil {
		t.Error("rejected edit must not call EdgeConfigurer")
	}
}
