package utils

import (
	"context"
	"testing"

	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/api/v1alpha1"
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
