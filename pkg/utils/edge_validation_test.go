package utils

import (
	"context"
	"testing"

	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/schema"
)

func TestValidateEdgeSchema(t *testing.T) {
	tests := []struct {
		name              string
		portSchema        string
		incomingPortData  interface{}
		edgeConfiguration string
		wantErr           bool
		errContains       string
	}{
		{
			name:              "nil schema - always valid",
			portSchema:        "",
			incomingPortData:  map[string]interface{}{"value": "test"},
			edgeConfiguration: `{"field": "test"}`,
			wantErr:           false,
		},
		{
			name:              "empty configuration uses empty object",
			portSchema:        `{"type": "object"}`,
			incomingPortData:  map[string]interface{}{},
			edgeConfiguration: "",
			wantErr:           false,
		},
		{
			name:              "valid static configuration",
			portSchema:        `{"type": "object", "properties": {"name": {"type": "string"}}}`,
			incomingPortData:  map[string]interface{}{},
			edgeConfiguration: `{"name": "test"}`,
			wantErr:           false,
		},
		{
			name:              "valid expression - root path",
			portSchema:        `{"type": "object", "properties": {"data": {"type": "string"}}}`,
			incomingPortData:  map[string]interface{}{"value": "hello"},
			edgeConfiguration: `{"data": "{{$.value}}"}`,
			wantErr:           false,
		},
		{
			name:              "valid expression - nested path",
			portSchema:        `{"type": "object", "properties": {"result": {"type": "string"}}}`,
			incomingPortData:  map[string]interface{}{"user": map[string]interface{}{"name": "Alice"}},
			edgeConfiguration: `{"result": "{{$.user.name}}"}`,
			wantErr:           false,
		},
		{
			name:              "valid expression - entire object with $",
			portSchema:        `{"type": "object"}`,
			incomingPortData:  map[string]interface{}{"key": "value"},
			edgeConfiguration: `{"context": "{{$}}"}`,
			wantErr:           false,
		},
		{
			name:              "invalid - wrong type",
			portSchema:        `{"type": "object", "properties": {"count": {"type": "integer"}}, "required": ["count"]}`,
			incomingPortData:  map[string]interface{}{},
			edgeConfiguration: `{"count": "not-a-number"}`,
			wantErr:           true,
		},
		{
			name:              "invalid - missing required field",
			portSchema:        `{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`,
			incomingPortData:  map[string]interface{}{},
			edgeConfiguration: `{}`,
			wantErr:           true,
		},
		{
			name:              "expression error - invalid path returns null which fails validation",
			portSchema:        `{"type": "object", "properties": {"data": {"type": "string"}}}`,
			incomingPortData:  map[string]interface{}{"value": "test"},
			edgeConfiguration: `{"data": "{{$.nonexistent.path}}"}`,
			wantErr:           true,
			errContains:       "expected string, but got null",
		},
		{
			name:              "expression preserves type - number",
			portSchema:        `{"type": "object", "properties": {"count": {"type": "number"}}}`,
			incomingPortData:  map[string]interface{}{"num": 42.0},
			edgeConfiguration: `{"count": "{{$.num}}"}`,
			wantErr:           false,
		},
		{
			name:              "expression preserves type - boolean",
			portSchema:        `{"type": "object", "properties": {"active": {"type": "boolean"}}}`,
			incomingPortData:  map[string]interface{}{"flag": true},
			edgeConfiguration: `{"active": "{{$.flag}}"}`,
			wantErr:           false,
		},
		{
			name:              "string interpolation",
			portSchema:        `{"type": "object", "properties": {"greeting": {"type": "string"}}}`,
			incomingPortData:  map[string]interface{}{"name": "World"},
			edgeConfiguration: `{"greeting": "Hello {{$.name}}!"}`,
			wantErr:           false,
		},
		{
			name:              "complex schema with $defs",
			portSchema:        `{"$ref": "#/$defs/Message", "$defs": {"Message": {"type": "object", "properties": {"body": {"type": "string"}}}}}`,
			incomingPortData:  map[string]interface{}{"text": "hello"},
			edgeConfiguration: `{"body": "{{$.text}}"}`,
			wantErr:           false,
		},
		{
			name:              "array in schema",
			portSchema:        `{"type": "object", "properties": {"items": {"type": "array", "items": {"type": "string"}}}}`,
			incomingPortData:  map[string]interface{}{"list": []interface{}{"a", "b", "c"}},
			edgeConfiguration: `{"items": "{{$.list}}"}`,
			wantErr:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var portSchema *ajson.Node
			if tt.portSchema != "" {
				var err error
				portSchema, err = ajson.Unmarshal([]byte(tt.portSchema))
				if err != nil {
					t.Fatalf("failed to parse port schema: %v", err)
				}
			}

			err := ValidateEdgeSchema(portSchema, tt.incomingPortData, []byte(tt.edgeConfiguration))

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEdgeSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !containsString(err.Error(), tt.errContains) {
					t.Errorf("ValidateEdgeSchema() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateEdgeWithSchemaAndRuntimeData(t *testing.T) {
	ctx := context.Background()

	// Create a simple flow with two connected nodes
	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source-component",
			Edges: []v1alpha1.TinyNodeEdge{
				{
					ID:   "edge-1",
					Port: "out",
					To:   "target-node:in",
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{
					Name:   "out",
					Source: true,
					Schema: []byte(`{"type": "object", "properties": {"message": {"type": "string"}}}`),
				},
			},
		},
	}
	sourceNode.Name = "source-node"

	targetNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "target-component",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:          "in",
					From:          "source-node:out",
					Configuration: []byte(`{"data": "{{$.message}}"}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{
					Name:   "in",
					Source: false,
					Schema: []byte(`{"type": "object", "properties": {"data": {"type": "string"}}}`),
				},
			},
		},
	}
	targetNode.Name = "target-node"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source-node": sourceNode,
		"target-node": targetNode,
	}

	tests := []struct {
		name              string
		sourcePortFull    string
		edgeConfiguration []byte
		edgeSchema        []byte
		runtimeData       map[string][]byte
		wantErr           bool
	}{
		{
			name:              "valid edge with simulated data",
			sourcePortFull:    "source-node:out",
			edgeConfiguration: []byte(`{"data": "{{$.message}}"}`),
			edgeSchema:        []byte(`{"type": "object", "properties": {"data": {"type": "string"}}}`),
			runtimeData:       nil,
			wantErr:           false,
		},
		{
			name:              "valid edge with runtime data",
			sourcePortFull:    "source-node:out",
			edgeConfiguration: []byte(`{"data": "{{$.message}}"}`),
			edgeSchema:        []byte(`{"type": "object", "properties": {"data": {"type": "string"}}}`),
			runtimeData: map[string][]byte{
				"source-node:out": []byte(`{"message": "hello from trace"}`),
			},
			wantErr: false,
		},
		{
			name:              "empty schema - always valid",
			sourcePortFull:    "source-node:out",
			edgeConfiguration: []byte(`{"anything": "goes"}`),
			edgeSchema:        nil,
			runtimeData:       nil,
			wantErr:           false,
		},
		{
			name:              "invalid schema JSON",
			sourcePortFull:    "source-node:out",
			edgeConfiguration: []byte(`{"data": "test"}`),
			edgeSchema:        []byte(`{invalid json`),
			runtimeData:       nil,
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEdgeWithSchemaAndRuntimeData(
				ctx,
				nodesMap,
				tt.sourcePortFull,
				tt.edgeConfiguration,
				tt.edgeSchema,
				tt.runtimeData,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEdgeWithSchemaAndRuntimeData() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEdgeWithRuntimeData(t *testing.T) {
	ctx := context.Background()

	// Create nodes with proper schemas
	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge-1", Port: "out", To: "target:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{
					Name:   "out",
					Source: true,
					Schema: []byte(`{"type": "object", "properties": {"value": {"type": "string"}}}`),
				},
			},
		},
	}
	sourceNode.Name = "source"

	targetNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "target",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:          "in",
					From:          "source:out",
					Configuration: []byte(`{"input": "{{$.value}}"}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{
					Name:   "in",
					Source: false,
					Schema: []byte(`{"type": "object", "properties": {"input": {"type": "string"}}}`),
				},
			},
		},
	}
	targetNode.Name = "target"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source": sourceNode,
		"target": targetNode,
	}

	tests := []struct {
		name              string
		sourcePortFull    string
		targetPortFull    string
		edgeConfiguration []byte
		runtimeData       map[string][]byte
		wantErr           bool
	}{
		{
			name:              "valid with simulated data",
			sourcePortFull:    "source:out",
			targetPortFull:    "target:in",
			edgeConfiguration: []byte(`{"input": "{{$.value}}"}`),
			runtimeData:       nil,
			wantErr:           false,
		},
		{
			name:              "valid with runtime data",
			sourcePortFull:    "source:out",
			targetPortFull:    "target:in",
			edgeConfiguration: []byte(`{"input": "{{$.value}}"}`),
			runtimeData: map[string][]byte{
				"source:out": []byte(`{"value": "runtime-value"}`),
			},
			wantErr: false,
		},
		{
			name:              "unknown target port - no schema to validate",
			sourcePortFull:    "source:out",
			targetPortFull:    "unknown:port",
			edgeConfiguration: []byte(`{"anything": "goes"}`),
			runtimeData:       nil,
			wantErr:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEdgeWithRuntimeData(
				ctx,
				nodesMap,
				tt.sourcePortFull,
				tt.targetPortFull,
				tt.edgeConfiguration,
				tt.runtimeData,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEdgeWithRuntimeData() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEdge(t *testing.T) {
	ctx := context.Background()

	// Simple wrapper test - main logic tested in ValidateEdgeWithRuntimeData
	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"type": "object"}`)},
			},
		},
	}
	sourceNode.Name = "source"

	targetNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "target",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Schema: []byte(`{"type": "object"}`)},
			},
		},
	}
	targetNode.Name = "target"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source": sourceNode,
		"target": targetNode,
	}

	err := ValidateEdge(ctx, nodesMap, "source:out", "target:in", []byte(`{}`))
	if err != nil {
		t.Errorf("ValidateEdge() unexpected error: %v", err)
	}
}

func TestValidateEdgeWithSchema(t *testing.T) {
	ctx := context.Background()

	// Simple wrapper test
	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"type": "object", "properties": {"msg": {"type": "string"}}}`)},
			},
		},
	}
	sourceNode.Name = "source"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source": sourceNode,
	}

	err := ValidateEdgeWithSchema(
		ctx,
		nodesMap,
		"source:out",
		[]byte(`{"data": "{{$.msg}}"}`),
		[]byte(`{"type": "object", "properties": {"data": {"type": "string"}}}`),
	)
	if err != nil {
		t.Errorf("ValidateEdgeWithSchema() unexpected error: %v", err)
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestValidateEdgeWithPrecomputedMaps_ConfigurableOverlay tests the full validation pipeline
// that the platform uses in buildGraphEvents: GetFlowMaps → GetConfigurableDefinitions →
// UpdateWithDefinitions → ValidateEdgeWithPrecomputedMaps.
// This catches bugs where configurable definition overlays (type, required, etc.) from the
// source node leak into the target schema and cause false validation errors.
func TestValidateEdgeWithPrecomputedMaps_ConfigurableOverlay(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		// Source node: the node whose output port feeds the edge
		sourceNodeName string
		sourcePortName string
		sourceSchema   string // schema for source output port
		// Target node: receives data via the edge
		targetNodeName string
		targetPortName string
		targetSchema   string // schema for target input port
		// Settings: spec.ports with schema (for configurable definitions)
		sourceSettingsSchema string // settings port schema on source node (optional)
		targetSettingsSchema string // settings port schema on target node (optional)
		// Edge configuration
		edgeConfiguration string
		// What we expect
		wantErr     bool
		errContains string
	}{
		// ============================================================
		// Bare Context + configurable overlay — the "firestore → debug" bug
		// ============================================================
		{
			name:           "bare context target: string mapped into context passes after overlay",
			sourceNodeName: "firestore-listener",
			sourcePortName: "error",
			sourceSchema: `{
				"$defs": {
					"Error": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"error":{"type":"string"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"collection":{"type":"string"},"namespace":{"type":"string"}}}
				},
				"$ref": "#/$defs/Error"
			}`,
			targetNodeName: "debug",
			targetPortName: "in",
			targetSchema: `{
				"$defs": {
					"In": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Inputdata"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context"},
					"Inputdata": {"configurable":true,"path":"$.data","title":"Data"}
				},
				"$ref": "#/$defs/In"
			}`,
			edgeConfiguration: `{"context":"{{$.error}}","data":"{{$}}"}`,
			wantErr:           false,
		},
		{
			name:           "bare context target: object mapped into context passes after overlay",
			sourceNodeName: "ticker",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"endpoints":{"type":"array","items":{"type":"object"}}}}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "debug",
			targetPortName: "in",
			targetSchema: `{
				"$defs": {
					"In": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Inputdata"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context"},
					"Inputdata": {"configurable":true,"path":"$.data","title":"Data"}
				},
				"$ref": "#/$defs/In"
			}`,
			edgeConfiguration: `{"context":"{{$.context}}","data":"{{$}}"}`,
			wantErr:           false,
		},
		{
			name:           "bare context target: null mapped into context passes",
			sourceNodeName: "source",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"value":{"type":"string"}},"type":"object"}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "debug",
			targetPortName: "in",
			targetSchema: `{
				"$defs": {
					"In": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Inputdata"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context"},
					"Inputdata": {"configurable":true,"path":"$.data","title":"Data"}
				},
				"$ref": "#/$defs/In"
			}`,
			edgeConfiguration: `{"context":null,"data":"{{$}}"}`,
			wantErr:           false,
		},
		// ============================================================
		// Required stripping — the "status page endpoints" bug
		// ============================================================
		{
			name:           "required stripped: target context without required field passes",
			sourceNodeName: "ticker",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"endpoints":{"type":"array","items":{"type":"object","properties":{"url":{"type":"string"},"method":{"type":"string"}}}},"project_name":{"type":"string"}},"required":["endpoints","project_name"]}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "http-request",
			targetPortName: "request",
			targetSchema: `{
				"$defs": {
					"Request": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"url":{"type":"string"},"method":{"type":"string"},"timeout":{"type":"integer"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context"}
				},
				"$ref": "#/$defs/Request"
			}`,
			// After split, individual endpoint item goes to http_request — no "endpoints" array
			edgeConfiguration: `{"context":"{{$.context}}","url":"{{$.context.endpoints[0].url}}","method":"GET","timeout":10}`,
			wantErr:           false,
		},
		// ============================================================
		// Typed Context target — validation SHOULD enforce type
		// ============================================================
		{
			name:           "typed context target: string value correctly rejected",
			sourceNodeName: "source",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"error":{"type":"string"}},"type":"object"}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "typed-target",
			targetPortName: "in",
			targetSchema: `{
				"$defs": {
					"Request": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"type":"string"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object"}
				},
				"$ref": "#/$defs/Request"
			}`,
			edgeConfiguration: `{"context":"{{$.error}}","data":"{{$.error}}"}`,
			wantErr:           true,
			errContains:       "expected object",
		},
		{
			name:           "typed context target: object value passes",
			sourceNodeName: "source",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"ctx":{"type":"object","properties":{"key":{"type":"string"}}}},"type":"object"}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "typed-target",
			targetPortName: "in",
			targetSchema: `{
				"$defs": {
					"Request": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object"}
				},
				"$ref": "#/$defs/Request"
			}`,
			edgeConfiguration: `{"context":"{{$.ctx}}"}`,
			wantErr:           false,
		},
		// ============================================================
		// Settings-based configurable overlay via path match (Startcontext)
		// ============================================================
		{
			name:           "path-matched overlay: HTTP server Start with configurable Startcontext",
			sourceNodeName: "ticker",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"hostname":{"type":"string"},"auto_hostname":{"type":"boolean"}}}
				},
				"$ref": "#/$defs/Output"
			}`,
			// sourceSettingsSchema provides the configurable Context definition
			// (in real world, ticker's _settings Spec.Port has the rich schema)
			sourceSettingsSchema: `{
				"$defs": {
					"Settings": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"delay":{"type":"integer"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"hostname":{"type":"string"},"auto_hostname":{"type":"boolean"}}}
				},
				"$ref": "#/$defs/Settings"
			}`,
			targetNodeName: "http-server",
			targetPortName: "start",
			targetSchema: `{
				"$defs": {
					"Start": {"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"},"port":{"type":"integer"},"readTimeout":{"type":"integer"}},"type":"object"},
					"Startcontext": {"additionalProperties":{"type":"string"},"configurable":true,"path":"$.context","title":"Context","type":"object"}
				},
				"$ref": "#/$defs/Start"
			}`,
			edgeConfiguration: `{"context":"{{$.context}}","port":8080,"readTimeout":30}`,
			wantErr:           false,
		},
		// ============================================================
		// Empty edge schema — always valid
		// ============================================================
		{
			name:           "empty edge schema: always valid",
			sourceNodeName: "source",
			sourcePortName: "out",
			sourceSchema:   `{"type":"object"}`,
			targetNodeName: "target",
			targetPortName: "in",
			targetSchema:   `{"type":"object"}`,
			edgeConfiguration: `{"anything":"goes"}`,
			wantErr:           false,
		},
		// ============================================================
		// No configurable defs — native schema used as-is
		// ============================================================
		{
			name:           "no configurable defs: native schema validates correctly",
			sourceNodeName: "source",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"message":{"type":"string"}},"type":"object"}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "target",
			targetPortName: "in",
			targetSchema: `{
				"$defs": {
					"Input": {"path":"$","properties":{"data":{"type":"string"},"count":{"type":"integer"}},"required":["data"],"type":"object"}
				},
				"$ref": "#/$defs/Input"
			}`,
			edgeConfiguration: `{"data":"{{$.message}}","count":1}`,
			wantErr:           false,
		},
		{
			name:           "no configurable defs: missing required field fails",
			sourceNodeName: "source",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"message":{"type":"string"}},"type":"object"}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "target",
			targetPortName: "in",
			targetSchema: `{
				"$defs": {
					"Input": {"path":"$","properties":{"data":{"type":"string"},"count":{"type":"integer"}},"required":["data"],"type":"object"}
				},
				"$ref": "#/$defs/Input"
			}`,
			edgeConfiguration: `{"count":1}`,
			wantErr:           true,
			errContains:       "missing properties",
		},
		// ============================================================
		// Expression error — bad path
		// ============================================================
		{
			name:           "expression error: nonexistent path returns null",
			sourceNodeName: "source",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"value":{"type":"string"}},"type":"object"}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "target",
			targetPortName: "in",
			targetSchema: `{
				"$defs": {
					"Input": {"path":"$","properties":{"data":{"type":"string"}},"type":"object"}
				},
				"$ref": "#/$defs/Input"
			}`,
			edgeConfiguration: `{"data":"{{$.nonexistent.deep.path}}"}`,
			wantErr:           true,
		},
		// ============================================================
		// Settings-based configurable definitions from target node's Spec.Ports
		// ============================================================
		{
			name:           "target settings schema provides configurable definitions for overlay",
			sourceNodeName: "ticker",
			sourcePortName: "out",
			sourceSchema: `{
				"$defs": {
					"Output": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"api_key":{"type":"string"}}}
				},
				"$ref": "#/$defs/Output"
			}`,
			targetNodeName: "api-client",
			targetPortName: "request",
			targetSchema: `{
				"$defs": {
					"Request": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"endpoint":{"type":"string"}},"type":"object"},
					"Context": {"configurable":true,"path":"$.context","title":"Context"}
				},
				"$ref": "#/$defs/Request"
			}`,
			// Context is bare on target, overlay from source adds type:object + properties
			// With type stripping, string should still pass
			edgeConfiguration: `{"context":"{{$.context}}","endpoint":"/api/v1/data"}`,
			wantErr:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build source node
			sourceNode := v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Component: tt.sourceNodeName + "-component",
					Edges: []v1alpha1.TinyNodeEdge{
						{
							ID:   "edge-1",
							Port: tt.sourcePortName,
							To:   tt.targetNodeName + ":" + tt.targetPortName,
						},
					},
				},
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{
						{
							Name:   tt.sourcePortName,
							Source: true,
							Schema: []byte(tt.sourceSchema),
						},
					},
				},
			}
			sourceNode.Name = tt.sourceNodeName

			// Build target node
			targetNode := v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Component: tt.targetNodeName + "-component",
					Ports: []v1alpha1.TinyNodePortConfig{
						{
							Port:          tt.targetPortName,
							From:          tt.sourceNodeName + ":" + tt.sourcePortName,
							Configuration: []byte(tt.edgeConfiguration),
						},
					},
				},
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{
						{
							Name:   tt.targetPortName,
							Source: false,
							Schema: []byte(tt.targetSchema),
						},
					},
				},
			}
			targetNode.Name = tt.targetNodeName

			// Add settings port schemas if provided
			if tt.sourceSettingsSchema != "" {
				sourceNode.Spec.Ports = append(sourceNode.Spec.Ports, v1alpha1.TinyNodePortConfig{
					Port:   "_settings",
					Schema: []byte(tt.sourceSettingsSchema),
				})
			}
			if tt.targetSettingsSchema != "" {
				targetNode.Spec.Ports = append(targetNode.Spec.Ports, v1alpha1.TinyNodePortConfig{
					Port:   "_settings",
					Schema: []byte(tt.targetSettingsSchema),
				})
			}

			nodesMap := map[string]v1alpha1.TinyNode{
				tt.sourceNodeName: sourceNode,
				tt.targetNodeName: targetNode,
			}

			// Step 1: Build flow maps (same as platform does)
			_, _, destinationsMap, portSchemaMap, _, err := GetFlowMaps(nodesMap)
			if err != nil {
				t.Fatalf("GetFlowMaps() error: %v", err)
			}

			// Step 2: Get configurable definitions from both nodes
			sourcePortFullName := tt.sourceNodeName + ":" + tt.sourcePortName
			targetPortFullName := tt.targetNodeName + ":" + tt.targetPortName
			defs := GetConfigurableDefinitions(sourceNode, nil)
			targetDefs := GetConfigurableDefinitions(targetNode, &sourcePortFullName)
			for k, v := range targetDefs {
				defs[k] = v
			}

			// Step 3: Get the target port's schema from portSchemaMap and apply overlay
			targetPortSchemaNode := portSchemaMap[targetPortFullName]
			if targetPortSchemaNode == nil {
				if tt.wantErr {
					return // no schema means no validation
				}
				t.Fatalf("target port schema not found in portSchemaMap for %s", targetPortFullName)
			}

			edgeSchemaBytes, err := ajson.Marshal(targetPortSchemaNode)
			if err != nil {
				t.Fatalf("Marshal target port schema: %v", err)
			}

			// Step 4: Apply configurable overlay (same as buildGraphEvents)
			overlaidSchema, err := schema.UpdateWithDefinitions(edgeSchemaBytes, defs)
			if err != nil {
				t.Fatalf("UpdateWithDefinitions() error: %v", err)
			}

			// Step 5: Validate using precomputed maps
			err = ValidateEdgeWithPrecomputedMaps(
				ctx,
				portSchemaMap,
				destinationsMap,
				sourcePortFullName,
				[]byte(tt.edgeConfiguration),
				overlaidSchema,
				nil, // no runtime data
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEdgeWithPrecomputedMaps() error = %v, wantErr %v\n  overlaid schema: %s", err, tt.wantErr, overlaidSchema)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !containsString(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

// TestValidateEdgeWithPrecomputedMaps_Basic tests the function with manually constructed maps.
func TestValidateEdgeWithPrecomputedMaps_Basic(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name              string
		portSchemaMap     map[string]string
		destinations      map[string][]Destination
		sourcePort        string
		edgeConfig        string
		edgeSchema        string
		runtimeData       map[string][]byte
		wantErr           bool
		errContains       string
	}{
		{
			name: "empty edge schema - always valid",
			portSchemaMap: map[string]string{
				"src:out": `{"type":"object","properties":{"x":{"type":"string"}}}`,
			},
			sourcePort: "src:out",
			edgeConfig: `{"y":"test"}`,
			edgeSchema: "",
			wantErr:    false,
		},
		{
			name: "valid static config against simple schema",
			portSchemaMap: map[string]string{
				"src:out": `{"type":"object","properties":{"msg":{"type":"string"}}}`,
			},
			sourcePort: "src:out",
			edgeConfig: `{"text":"hello"}`,
			edgeSchema: `{"type":"object","properties":{"text":{"type":"string"}}}`,
			wantErr:    false,
		},
		{
			name: "valid expression resolves correctly",
			portSchemaMap: map[string]string{
				"src:out": `{"type":"object","properties":{"msg":{"type":"string"}}}`,
			},
			sourcePort: "src:out",
			edgeConfig: `{"text":"{{$.msg}}"}`,
			edgeSchema: `{"type":"object","properties":{"text":{"type":"string"}}}`,
			wantErr:    false,
		},
		{
			name: "type mismatch - expression resolves to wrong type",
			portSchemaMap: map[string]string{
				"src:out": `{"type":"object","properties":{"count":{"type":"integer"}}}`,
			},
			sourcePort: "src:out",
			edgeConfig: `{"text":"{{$.count}}"}`,
			edgeSchema: `{"type":"object","properties":{"text":{"type":"string"}}}`,
			wantErr:    true,
			errContains: "expected string",
		},
		{
			name: "runtime data overrides simulated data",
			portSchemaMap: map[string]string{
				"src:out": `{"type":"object","properties":{"msg":{"type":"string"}}}`,
			},
			sourcePort: "src:out",
			edgeConfig: `{"text":"{{$.msg}}"}`,
			edgeSchema: `{"type":"object","properties":{"text":{"type":"string"}}}`,
			runtimeData: map[string][]byte{
				"src:out": []byte(`{"msg":"runtime value"}`),
			},
			wantErr: false,
		},
		{
			name: "schema with $defs and $ref",
			portSchemaMap: map[string]string{
				"src:out": `{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"integer"}}}`,
			},
			sourcePort: "src:out",
			edgeConfig: `{"user_name":"{{$.name}}","user_age":"{{$.age}}"}`,
			edgeSchema: `{"$defs":{"User":{"type":"object","properties":{"user_name":{"type":"string"},"user_age":{"type":"integer"}}}},"$ref":"#/$defs/User"}`,
			wantErr:    false,
		},
		{
			name: "missing required field in edge config",
			portSchemaMap: map[string]string{
				"src:out": `{"type":"object","properties":{"x":{"type":"string"}}}`,
			},
			sourcePort: "src:out",
			edgeConfig: `{}`,
			edgeSchema: `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`,
			wantErr:    true,
			errContains: "missing properties",
		},
		{
			name: "invalid edge schema JSON",
			portSchemaMap: map[string]string{
				"src:out": `{"type":"object"}`,
			},
			sourcePort: "src:out",
			edgeConfig: `{}`,
			edgeSchema: `{invalid json`,
			wantErr:    true,
			errContains: "invalid edge schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build portSchemaMap from strings
			psm := make(map[string]*ajson.Node)
			for k, v := range tt.portSchemaMap {
				node, err := ajson.Unmarshal([]byte(v))
				if err != nil {
					t.Fatalf("invalid port schema for %s: %v", k, err)
				}
				psm[k] = node
			}

			destinations := tt.destinations
			if destinations == nil {
				destinations = make(map[string][]Destination)
			}

			var edgeSchemaBytes []byte
			if tt.edgeSchema != "" {
				edgeSchemaBytes = []byte(tt.edgeSchema)
			}

			err := ValidateEdgeWithPrecomputedMaps(
				ctx,
				psm,
				destinations,
				tt.sourcePort,
				[]byte(tt.edgeConfig),
				edgeSchemaBytes,
				tt.runtimeData,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !containsString(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestCrossValidateEdgeSchemaKeys(t *testing.T) {
	tests := []struct {
		name             string
		edgeSchema       string
		targetPortSchema string
		wantMismatches   int
		wantEdgeKey      string
		wantExpectedKey  string
		wantDefName      string
	}{
		{
			name: "matching schemas - no mismatches",
			edgeSchema: `{
				"$defs": {
					"Condition": {
						"type": "object",
						"properties": {
							"route": {"type": "string"},
							"condition": {"type": "boolean"}
						}
					}
				},
				"$ref": "#/$defs/Condition"
			}`,
			targetPortSchema: `{
				"$defs": {
					"Condition": {
						"type": "object",
						"properties": {
							"route": {"type": "string"},
							"condition": {"type": "boolean"}
						}
					}
				},
				"$ref": "#/$defs/Condition"
			}`,
			wantMismatches: 0,
		},
		{
			name: "routeName vs route mismatch",
			edgeSchema: `{
				"$defs": {
					"Condition": {
						"type": "object",
						"properties": {
							"routeName": {"type": "string"},
							"condition": {"type": "boolean"}
						}
					}
				},
				"$ref": "#/$defs/Condition"
			}`,
			targetPortSchema: `{
				"$defs": {
					"Condition": {
						"type": "object",
						"properties": {
							"route": {"type": "string"},
							"condition": {"type": "boolean"}
						}
					}
				},
				"$ref": "#/$defs/Condition"
			}`,
			wantMismatches:  1,
			wantEdgeKey:     "routeName",
			wantExpectedKey: "", // no case-insensitive match for routeName→route
			wantDefName:     "Condition",
		},
		{
			name: "case mismatch suggests expected key",
			edgeSchema: `{
				"$defs": {
					"Settings": {
						"type": "object",
						"properties": {
							"Delay": {"type": "integer"}
						}
					}
				},
				"$ref": "#/$defs/Settings"
			}`,
			targetPortSchema: `{
				"$defs": {
					"Settings": {
						"type": "object",
						"properties": {
							"delay": {"type": "integer"}
						}
					}
				},
				"$ref": "#/$defs/Settings"
			}`,
			wantMismatches:  1,
			wantEdgeKey:     "Delay",
			wantExpectedKey: "delay",
			wantDefName:     "Settings",
		},
		{
			name: "extra properties in edge only",
			edgeSchema: `{
				"$defs": {
					"Message": {
						"type": "object",
						"properties": {
							"body": {"type": "string"},
							"extraField": {"type": "string"}
						}
					}
				},
				"$ref": "#/$defs/Message"
			}`,
			targetPortSchema: `{
				"$defs": {
					"Message": {
						"type": "object",
						"properties": {
							"body": {"type": "string"}
						}
					}
				},
				"$ref": "#/$defs/Message"
			}`,
			wantMismatches: 1,
			wantEdgeKey:    "extraField",
			wantDefName:    "Message",
		},
		{
			name:             "nil edge schema",
			edgeSchema:       "",
			targetPortSchema: `{"$defs": {"X": {"type": "object", "properties": {"a": {"type": "string"}}}}}`,
			wantMismatches:   0,
		},
		{
			name:             "nil target schema",
			edgeSchema:       `{"$defs": {"X": {"type": "object", "properties": {"a": {"type": "string"}}}}}`,
			targetPortSchema: "",
			wantMismatches:   0,
		},
		{
			name: "def only in edge - no comparison",
			edgeSchema: `{
				"$defs": {
					"OnlyInEdge": {
						"type": "object",
						"properties": {"foo": {"type": "string"}}
					}
				}
			}`,
			targetPortSchema: `{
				"$defs": {
					"OnlyInTarget": {
						"type": "object",
						"properties": {"bar": {"type": "string"}}
					}
				}
			}`,
			wantMismatches: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var edgeSchema, targetPortSchema *ajson.Node
			if tt.edgeSchema != "" {
				var err error
				edgeSchema, err = ajson.Unmarshal([]byte(tt.edgeSchema))
				if err != nil {
					t.Fatalf("invalid edge schema: %v", err)
				}
			}
			if tt.targetPortSchema != "" {
				var err error
				targetPortSchema, err = ajson.Unmarshal([]byte(tt.targetPortSchema))
				if err != nil {
					t.Fatalf("invalid target port schema: %v", err)
				}
			}

			mismatches := CrossValidateEdgeSchemaKeys(edgeSchema, targetPortSchema)

			if len(mismatches) != tt.wantMismatches {
				t.Fatalf("got %d mismatches, want %d: %+v", len(mismatches), tt.wantMismatches, mismatches)
			}

			if tt.wantMismatches > 0 {
				m := mismatches[0]
				if tt.wantEdgeKey != "" && m.EdgeKey != tt.wantEdgeKey {
					t.Errorf("EdgeKey = %q, want %q", m.EdgeKey, tt.wantEdgeKey)
				}
				if tt.wantExpectedKey != "" && m.ExpectedKey != tt.wantExpectedKey {
					t.Errorf("ExpectedKey = %q, want %q", m.ExpectedKey, tt.wantExpectedKey)
				}
				if tt.wantDefName != "" && m.DefName != tt.wantDefName {
					t.Errorf("DefName = %q, want %q", m.DefName, tt.wantDefName)
				}
			}
		})
	}
}
