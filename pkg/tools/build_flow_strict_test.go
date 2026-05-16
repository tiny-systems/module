package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ----- mocks -----

// mockModuleCatalog returns the same single component for any module
// lookup. Components are keyed by component-name within a single
// "tinysystems/test-module" namespace so tests can compose whichever
// shape they need.
type mockModuleCatalog struct {
	components map[string]ComponentInfo
}

func (m *mockModuleCatalog) ListModules(_ context.Context) ([]ModuleInfo, error) {
	return []ModuleInfo{m.module()}, nil
}

func (m *mockModuleCatalog) GetModule(_ context.Context, _ string) (*ModuleInfo, error) {
	mod := m.module()
	return &mod, nil
}

func (m *mockModuleCatalog) module() ModuleInfo {
	comps := make([]ComponentInfo, 0, len(m.components))
	for _, c := range m.components {
		comps = append(comps, c)
	}
	return ModuleInfo{
		Name:       "tinysystems/test-module",
		Components: comps,
	}
}

// countingNodeAdder records how many times AddNode was called so the
// strict tests can assert "rejected before any mutation."
type countingNodeAdder struct {
	calls int
}

func (c *countingNodeAdder) AddNode(_ context.Context, _, _, _, _ string, _ PositionTracker) (*AddNodeResult, error) {
	c.calls++
	return &AddNodeResult{NodeID: "n1", Ports: []string{"_settings", "request", "response"}}, nil
}

// countingEdgeAdder is the EdgeAdder counterpart.
type countingEdgeAdder struct {
	calls int
}

func (c *countingEdgeAdder) AddEdge(_ context.Context, _, _, _, _, _, _ string) (*AddEdgeResult, error) {
	c.calls++
	return &AddEdgeResult{EdgeID: "e1"}, nil
}

// settingsSchemaWithConfigurableContext returns a JSON Schema in the
// platform-native shape: a Settings object with one configurable
// "Context" def. The pre-flight looks for `$defs.*.configurable=true`
// and infers the JSON field name from the def's first letter
// lowercased — so this declares one configurable field named
// "context".
func settingsSchemaWithConfigurableContext() json.RawMessage {
	return json.RawMessage(`{
		"$defs": {
			"Context": {
				"configurable": true,
				"type": "object",
				"properties": {"token": {"type": "string"}}
			},
			"Settings": {
				"properties": {
					"context": {"$ref": "#/$defs/Context"},
					"delay": {"type": "integer"}
				},
				"type": "object"
			}
		},
		"$ref": "#/$defs/Settings"
	}`)
}

// targetPortSchemaWithConfigurableContext mirrors the above but used
// for an edge target port (e.g. an http_request's request port).
func targetPortSchemaWithConfigurableContext() json.RawMessage {
	return json.RawMessage(`{
		"$defs": {
			"Context": {
				"configurable": true,
				"type": "object",
				"properties": {"apiKey": {"type": "string"}}
			},
			"Request": {
				"properties": {
					"context": {"$ref": "#/$defs/Context"},
					"url":     {"type": "string"}
				},
				"type": "object"
			}
		},
		"$ref": "#/$defs/Request"
	}`)
}

// settingsSchemaPlain has no configurable defs. The strictness check
// must NOT reject calls that fill these.
func settingsSchemaPlain() json.RawMessage {
	return json.RawMessage(`{
		"$defs": {
			"Settings": {
				"type": "object",
				"properties": {"delay": {"type": "integer"}}
			}
		},
		"$ref": "#/$defs/Settings"
	}`)
}

// ----- tests -----

// TestBuildFlow_RejectsMissingSettingsSchemaForConfigurableField pins
// the load-bearing strict check: if a node fills a configurable
// settings field, settings_schema must declare its shape — and the
// rejection happens before any cluster mutation.
func TestBuildFlow_RejectsMissingSettingsSchemaForConfigurableField(t *testing.T) {
	adder := &countingNodeAdder{}
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
	tool := NewBuildFlowTool()

	res := tool.Execute(context.Background(), ExecutionContext{
		ProjectName:   "p1",
		FlowName:      "f1",
		ModuleCatalog: catalog,
		NodeAdder:     adder,
		EdgeAdder:     &countingEdgeAdder{},
	}, map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{
				"alias":     "tick",
				"component": "ticker",
				"module":    "tinysystems/test-module",
				"settings": map[string]interface{}{
					"context": map[string]interface{}{"token": "secret"},
				},
				// no settings_schema — strictness MUST reject
			},
		},
	})

	if res.Success {
		t.Fatalf("expected rejection, got success: %v", res.Output)
	}
	if !strings.Contains(res.Error, "context") {
		t.Errorf("error should name the missing field 'context'; got: %s", res.Error)
	}
	if adder.calls != 0 {
		t.Errorf("rejected build_flow must not call AddNode (called %d times)", adder.calls)
	}
}

// TestBuildFlow_AcceptsExplicitSchemaForConfigurableField proves the
// happy path: providing settings_schema for a configurable field lets
// the call through and the node gets created.
func TestBuildFlow_AcceptsExplicitSchemaForConfigurableField(t *testing.T) {
	adder := &countingNodeAdder{}
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
	tool := NewBuildFlowTool()

	res := tool.Execute(context.Background(), ExecutionContext{
		ProjectName:            "p1",
		FlowName:               "f1",
		ModuleCatalog:          catalog,
		NodeAdder:              adder,
		EdgeAdder:              &countingEdgeAdder{},
		NodeSettingsConfigurer: &captureNodeSettingsConfigurer{},
	}, map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{
				"alias":     "tick",
				"component": "ticker",
				"module":    "tinysystems/test-module",
				"settings": map[string]interface{}{
					"context": map[string]interface{}{"token": "secret"},
				},
				"settings_schema": map[string]interface{}{
					"context": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"token": map[string]interface{}{"type": "string"}},
					},
				},
			},
		},
	})

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if adder.calls != 1 {
		t.Errorf("expected AddNode to be called exactly once, got %d", adder.calls)
	}
}

// TestBuildFlow_RejectsMissingEdgeSchemaForConfigurableTargetField is
// the edge-side mirror: filling a configurable target field in edge
// configuration without an `edge.schema` entry must reject.
func TestBuildFlow_RejectsMissingEdgeSchemaForConfigurableTargetField(t *testing.T) {
	nodeAdder := &countingNodeAdder{}
	edgeAdder := &countingEdgeAdder{}
	catalog := &mockModuleCatalog{components: map[string]ComponentInfo{
		"emitter": {
			Name:        "emitter",
			InputPorts:  []string{"_settings"},
			OutputPorts: []string{"out"},
		},
		"receiver": {
			Name:        "receiver",
			InputPorts:  []string{"_settings", "request"},
			OutputPorts: []string{"response"},
			InputPortDetails: []PortDetail{
				{Name: "request", Schema: targetPortSchemaWithConfigurableContext()},
			},
		},
	}}
	tool := NewBuildFlowTool()

	res := tool.Execute(context.Background(), ExecutionContext{
		ProjectName:   "p1",
		FlowName:      "f1",
		ModuleCatalog: catalog,
		NodeAdder:     nodeAdder,
		EdgeAdder:     edgeAdder,
	}, map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{"alias": "src", "component": "emitter", "module": "tinysystems/test-module"},
			map[string]interface{}{"alias": "dst", "component": "receiver", "module": "tinysystems/test-module"},
		},
		"edges": []interface{}{
			map[string]interface{}{
				"from": "src:out", "to": "dst:request",
				"configuration": map[string]interface{}{
					"context": map[string]interface{}{"apiKey": "k1"},
					"url":     "{{$.url}}",
				},
				// no schema — strictness MUST reject
			},
		},
	})

	if res.Success {
		t.Fatalf("expected rejection, got success: %v", res.Output)
	}
	if !strings.Contains(res.Error, "context") {
		t.Errorf("error should name the missing field 'context'; got: %s", res.Error)
	}
	if nodeAdder.calls != 0 || edgeAdder.calls != 0 {
		t.Errorf("rejection must happen before any cluster mutation (nodes=%d, edges=%d)",
			nodeAdder.calls, edgeAdder.calls)
	}
}

// TestBuildFlow_NoRejectForNonConfigurableFields proves the
// strictness check does NOT fire when settings fill plain
// (non-configurable) fields. Strict pre-flight should only block on
// configurable-any gaps, not on any settings whatsoever.
func TestBuildFlow_NoRejectForNonConfigurableFields(t *testing.T) {
	adder := &countingNodeAdder{}
	catalog := &mockModuleCatalog{components: map[string]ComponentInfo{
		"ticker": {
			Name:        "ticker",
			InputPorts:  []string{"_settings"},
			OutputPorts: []string{"out"},
			InputPortDetails: []PortDetail{
				{Name: "_settings", Schema: settingsSchemaPlain()},
			},
		},
	}}
	tool := NewBuildFlowTool()

	res := tool.Execute(context.Background(), ExecutionContext{
		ProjectName:            "p1",
		FlowName:               "f1",
		ModuleCatalog:          catalog,
		NodeAdder:              adder,
		EdgeAdder:              &countingEdgeAdder{},
		NodeSettingsConfigurer: &captureNodeSettingsConfigurer{},
	}, map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{
				"alias":     "tick",
				"component": "ticker",
				"module":    "tinysystems/test-module",
				"settings":  map[string]interface{}{"delay": 1000},
				// no settings_schema — but no configurable fields either, so OK
			},
		},
	})

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if adder.calls != 1 {
		t.Errorf("expected AddNode to be called once, got %d", adder.calls)
	}
}
