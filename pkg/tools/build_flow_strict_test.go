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

// TestBuildFlow_DerivesMissingSettingsSchemaForConfigurableField: a node
// filling a configurable settings field without settings_schema is no
// longer rejected — settings are literal data, so the schema is derived
// from the value types and the build proceeds.
func TestBuildFlow_DerivesMissingSettingsSchemaForConfigurableField(t *testing.T) {
	adder := &countingNodeAdder{}
	settingsCfg := &captureNodeSettingsConfigurer{}
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
		NodeSettingsConfigurer: settingsCfg,
	}, map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{
				"alias":     "tick",
				"component": "ticker",
				"module":    "tinysystems/test-module",
				"settings": map[string]interface{}{
					"context": map[string]interface{}{"token": "secret"},
				},
				// no settings_schema — derived from literal value types
			},
		},
	})

	if !res.Success {
		t.Fatalf("expected success with derived settings schema, got: %s", res.Error)
	}
	if adder.calls != 1 {
		t.Errorf("expected AddNode once, got %d", adder.calls)
	}
	ctxSchema, _ := settingsCfg.gotSchema["context"].(map[string]interface{})
	if ctxSchema == nil {
		t.Fatalf("configurer should receive a derived schema for context; got %v", settingsCfg.gotSchema)
	}
	props, _ := ctxSchema["properties"].(map[string]interface{})
	if got := props["token"].(map[string]interface{})["type"]; got != "string" {
		t.Errorf("literal token should derive type string, got %v", got)
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

// TestBuildFlow_DerivesMissingEdgeSchemaForConfigurableTargetField is
// the edge-side mirror: filling a configurable target field in edge
// configuration without an `edge.schema` entry derives one (literals
// typed, unresolvable expressions untyped) and the build proceeds.
func TestBuildFlow_DerivesMissingEdgeSchemaForConfigurableTargetField(t *testing.T) {
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
				// no schema — derived instead of rejected
			},
		},
	})

	if !res.Success {
		t.Fatalf("expected success with derived edge schema, got: %s", res.Error)
	}
	if nodeAdder.calls != 2 || edgeAdder.calls != 1 {
		t.Errorf("build should proceed (nodes=%d, edges=%d)", nodeAdder.calls, edgeAdder.calls)
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

// TestBuildFlow_RejectsUnknownSetting pins the anti-guessing check: a
// setting name the component doesn't declare (the classic "script in a
// `code` field") is rejected before any cluster mutation, and the error
// names the offending key plus the valid ones.
func TestBuildFlow_RejectsUnknownSetting(t *testing.T) {
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
				"settings":  map[string]interface{}{"code": "oops"}, // schema only declares `delay`
			},
		},
	})

	if res.Success {
		t.Fatalf("expected rejection for unknown setting, got success: %v", res.Output)
	}
	if !strings.Contains(res.Error, "code") || !strings.Contains(res.Error, "delay") {
		t.Errorf("error should name the unknown key 'code' and the valid 'delay'; got: %s", res.Error)
	}
	if adder.calls != 0 {
		t.Errorf("rejected build_flow must not call AddNode (called %d times)", adder.calls)
	}
}

// TestBuildFlow_AcceptsKnownSetting is the happy path for the same check:
// a declared setting name passes the unknown-field gate.
func TestBuildFlow_AcceptsKnownSetting(t *testing.T) {
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
				"settings":  map[string]interface{}{"delay": 1000},
			},
		},
	})

	if !res.Success {
		t.Fatalf("known setting 'delay' must pass the unknown-field gate; got error: %s", res.Error)
	}
}
