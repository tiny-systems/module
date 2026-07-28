package tools

import (
	"context"
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

// TestEditFlow_ConfigureNode_DerivesMissingSchemaForConfigurableField:
// omitting schema for a configurable settings field no longer rejects —
// settings are literal data, so the schema is derived from value types
// and handed to NodeSettingsConfigurer.
func TestEditFlow_ConfigureNode_DerivesMissingSchemaForConfigurableField(t *testing.T) {
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
		// no "schema" — derived from literal value types
	})

	if !res.Success {
		t.Fatalf("expected success with derived schema, got: %s", res.Error)
	}
	ctxSchema, _ := cfg.gotSchema["context"].(map[string]interface{})
	if ctxSchema == nil {
		t.Fatalf("configurer should receive a derived schema for context; got %v", cfg.gotSchema)
	}
	props, _ := ctxSchema["properties"].(map[string]interface{})
	if got := props["token"].(map[string]interface{})["type"]; got != "string" {
		t.Errorf("literal token should derive type string, got %v", got)
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

// TestEditFlow_ConfigureEdge_DerivesMissingSchemaForConfigurableTargetField:
// filling a configurable target field without an `edge.schema` entry no
// longer rejects — the schema is DERIVED from the configuration (literals
// keep their types, expressions stay untyped) and passed through to the
// configurer. The "declare a schema for context on every edge" tax is gone;
// what must never come back is the removed value-shape inference that typed
// template strings as "string".
func TestEditFlow_ConfigureEdge_DerivesMissingSchemaForConfigurableTargetField(t *testing.T) {
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
			"context": map[string]interface{}{"apiKey": "k1", "n": float64(2), "tpl": "{{$.x}}"},
		},
		// no schema — derived instead of rejected
	})

	if !res.Success {
		t.Fatalf("expected success with derived schema, got: %s", res.Error)
	}
	ctxSchema, ok := cfg.gotSchema["context"].(map[string]interface{})
	if !ok {
		t.Fatalf("configurer should receive a derived schema for context; got: %v", cfg.gotSchema)
	}
	props, _ := ctxSchema["properties"].(map[string]interface{})
	if props == nil {
		t.Fatalf("derived context schema has no properties: %v", ctxSchema)
	}
	if got := props["apiKey"].(map[string]interface{})["type"]; got != "string" {
		t.Errorf("literal apiKey should derive type string, got %v", got)
	}
	if got := props["n"].(map[string]interface{})["type"]; got != "number" {
		t.Errorf("literal n should derive type number, got %v", got)
	}
	// The template expression must NOT be typed as string (the removed
	// value-shape inference's sin) — untyped {} is the contract.
	if tpl, _ := props["tpl"].(map[string]interface{}); len(tpl) != 0 {
		t.Errorf("unresolvable expression should stay untyped {}, got %v", tpl)
	}
}
