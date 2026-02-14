package utils

import (
	"context"
	"testing"

	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/api/v1alpha1"
)

func TestSimulatePortDataFromMaps(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		portSchemaMap  map[string]*ajson.Node
		edgeConfigMap  map[string][]Destination
		inspectPort    string
		runtimeData    map[string][]byte
		wantNil        bool
		wantErr        bool
		checkResult    func(interface{}) bool
	}{
		{
			name: "simple port with string schema",
			portSchemaMap: map[string]*ajson.Node{
				"node:port": mustUnmarshal(`{"type": "string"}`),
			},
			edgeConfigMap:  map[string][]Destination{},
			inspectPort:    "node:port",
			runtimeData:    nil,
			wantNil:        false,
			wantErr:        false,
			checkResult: func(result interface{}) bool {
				_, ok := result.(string)
				return ok
			},
		},
		{
			name: "port with object schema",
			portSchemaMap: map[string]*ajson.Node{
				"node:port": mustUnmarshal(`{"type": "object", "properties": {"name": {"type": "string"}, "count": {"type": "integer"}}}`),
			},
			edgeConfigMap:  map[string][]Destination{},
			inspectPort:    "node:port",
			runtimeData:    nil,
			wantNil:        false,
			wantErr:        false,
			checkResult: func(result interface{}) bool {
				m, ok := result.(map[string]interface{})
				if !ok {
					return false
				}
				_, hasName := m["name"]
				_, hasCount := m["count"]
				return hasName && hasCount
			},
		},
		{
			name: "port with runtime data overrides simulated",
			portSchemaMap: map[string]*ajson.Node{
				"node:port": mustUnmarshal(`{"type": "object", "properties": {"message": {"type": "string"}}}`),
			},
			edgeConfigMap: map[string][]Destination{},
			inspectPort:   "node:port",
			runtimeData: map[string][]byte{
				"node:port": []byte(`{"message": "from runtime"}`),
			},
			wantNil: false,
			wantErr: false,
			checkResult: func(result interface{}) bool {
				m, ok := result.(map[string]interface{})
				if !ok {
					return false
				}
				return m["message"] == "from runtime"
			},
		},
		{
			name: "port visited in trace but with empty data returns nil",
			portSchemaMap: map[string]*ajson.Node{
				"node:port": mustUnmarshal(`{"type": "object"}`),
			},
			edgeConfigMap: map[string][]Destination{},
			inspectPort:   "node:port",
			runtimeData: map[string][]byte{
				"node:port": []byte(``), // empty - port was visited but no data
			},
			wantNil: true,
			wantErr: false,
		},
		{
			name:           "unknown port returns null node",
			portSchemaMap:  map[string]*ajson.Node{},
			edgeConfigMap:  map[string][]Destination{},
			inspectPort:    "unknown:port",
			runtimeData:    nil,
			wantNil:        true,
			wantErr:        false,
		},
		{
			name: "port with array schema",
			portSchemaMap: map[string]*ajson.Node{
				"node:port": mustUnmarshal(`{"type": "array", "items": {"type": "string"}}`),
			},
			edgeConfigMap:  map[string][]Destination{},
			inspectPort:    "node:port",
			runtimeData:    nil,
			wantNil:        false,
			wantErr:        false,
			checkResult: func(result interface{}) bool {
				_, ok := result.([]interface{})
				return ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SimulatePortDataFromMaps(
				ctx,
				tt.portSchemaMap,
				tt.edgeConfigMap,
				tt.inspectPort,
				tt.runtimeData,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("SimulatePortDataFromMaps() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantNil && result != nil {
				t.Errorf("SimulatePortDataFromMaps() = %v, want nil", result)
				return
			}

			if !tt.wantNil && tt.checkResult != nil && !tt.checkResult(result) {
				t.Errorf("SimulatePortDataFromMaps() result check failed, got %v", result)
			}
		})
	}
}

func TestSimulatePortData(t *testing.T) {
	ctx := context.Background()

	// Create a flow with connected nodes
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
					Configuration: []byte(`{"data": "{{$.value}}"}`),
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
	targetNode.Name = "target"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source": sourceNode,
		"target": targetNode,
	}

	t.Run("simulate source port", func(t *testing.T) {
		result, err := SimulatePortData(ctx, nodesMap, "source:out", nil)
		if err != nil {
			t.Fatalf("SimulatePortData() error = %v", err)
		}

		m, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}

		if _, hasValue := m["value"]; !hasValue {
			t.Error("expected 'value' field in result")
		}
	})

	t.Run("simulate with runtime data", func(t *testing.T) {
		runtimeData := map[string][]byte{
			"source:out": []byte(`{"value": "runtime-value"}`),
		}

		result, err := SimulatePortData(ctx, nodesMap, "source:out", runtimeData)
		if err != nil {
			t.Fatalf("SimulatePortData() error = %v", err)
		}

		m, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}

		if m["value"] != "runtime-value" {
			t.Errorf("expected 'runtime-value', got %v", m["value"])
		}
	})
}

func TestSimulatePortDataSimple(t *testing.T) {
	ctx := context.Background()

	node := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "test",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{
					Name:   "out",
					Source: true,
					Schema: []byte(`{"type": "object", "properties": {"msg": {"type": "string"}}}`),
				},
			},
		},
	}
	node.Name = "test-node"

	nodesMap := map[string]v1alpha1.TinyNode{
		"test-node": node,
	}

	result, err := SimulatePortDataSimple(ctx, nodesMap, "test-node:out")
	if err != nil {
		t.Fatalf("SimulatePortDataSimple() error = %v", err)
	}

	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestGetPortHandles(t *testing.T) {
	node := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "test",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Label: "Input", Position: 0},
				{Name: "out", Source: true, Label: "Output", Position: 2},
				{Name: v1alpha1.SettingsPort, Source: false, Label: "Settings", Position: 3},
				{Name: v1alpha1.ControlPort, Source: true, Label: "Control", Position: 1},
			},
		},
	}
	node.Name = "test-node"

	t.Run("exclude system ports", func(t *testing.T) {
		handles := GetPortHandles(node, false)

		if len(handles) != 2 {
			t.Errorf("expected 2 handles, got %d", len(handles))
		}

		for _, h := range handles {
			id := h["id"].(string)
			if id == v1alpha1.SettingsPort || id == v1alpha1.ControlPort {
				t.Errorf("system port %s should be excluded", id)
			}
		}
	})

	t.Run("include system ports", func(t *testing.T) {
		handles := GetPortHandles(node, true)

		if len(handles) != 4 {
			t.Errorf("expected 4 handles, got %d", len(handles))
		}

		foundSettings := false
		foundControl := false
		for _, h := range handles {
			id := h["id"].(string)
			if id == v1alpha1.SettingsPort {
				foundSettings = true
			}
			if id == v1alpha1.ControlPort {
				foundControl = true
			}
		}

		if !foundSettings {
			t.Error("expected to find _settings port")
		}
		if !foundControl {
			t.Error("expected to find _control port")
		}
	})
}

func TestGetAllPortHandles(t *testing.T) {
	node := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "test",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "port1", Source: false, Label: "Port 1"},
				{Name: v1alpha1.SettingsPort, Source: false, Label: "Settings"},
			},
		},
	}
	node.Name = "test"

	handles := GetAllPortHandles(node)

	if len(handles) != 2 {
		t.Errorf("expected 2 handles, got %d", len(handles))
	}
}

func TestSimulatePortDataWithEdgeChain(t *testing.T) {
	ctx := context.Background()

	// Create a chain: node1 -> node2 -> node3
	// This tests that simulation follows edges correctly
	node1 := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "start",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "e1", Port: "out", To: "node2:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{
					Name:   "out",
					Source: true,
					Schema: []byte(`{"type": "object", "properties": {"initial": {"type": "string"}}}`),
				},
			},
		},
	}
	node1.Name = "node1"

	node2 := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "middle",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "e2", Port: "out", To: "node3:in"},
			},
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:          "in",
					From:          "node1:out",
					Configuration: []byte(`{"value": "{{$.initial}}"}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{
					Name:   "in",
					Source: false,
					Schema: []byte(`{"type": "object", "properties": {"value": {"type": "string"}}}`),
				},
				{
					Name:   "out",
					Source: true,
					Schema: []byte(`{"type": "object", "properties": {"processed": {"type": "string"}}}`),
				},
			},
		},
	}
	node2.Name = "node2"

	node3 := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "end",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:          "in",
					From:          "node2:out",
					Configuration: []byte(`{"final": "{{$.processed}}"}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{
					Name:   "in",
					Source: false,
					Schema: []byte(`{"type": "object", "properties": {"final": {"type": "string"}}}`),
				},
			},
		},
	}
	node3.Name = "node3"

	nodesMap := map[string]v1alpha1.TinyNode{
		"node1": node1,
		"node2": node2,
		"node3": node3,
	}

	t.Run("simulate first node port", func(t *testing.T) {
		result, err := SimulatePortData(ctx, nodesMap, "node1:out", nil)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("simulate middle node port", func(t *testing.T) {
		result, err := SimulatePortData(ctx, nodesMap, "node2:out", nil)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if result == nil {
			t.Error("expected non-nil result")
		}
	})
}

// TestSimulatePortData_ConfigurableDefinitionOrder verifies that simulating a
// source port with configurable definitions produces non-null fake data, even
// when the source port name sorts alphabetically before the target port that
// provides the definition (e.g. query_result before store).
//
// This is the end-to-end test for the two-pass definition replacement fix.
func TestSimulatePortData_ConfigurableDefinitionOrder(t *testing.T) {
	ctx := context.Background()

	settingsSchema := []byte(`{
		"$defs": {
			"Document": {
				"type": "object",
				"configurable": true,
				"properties": {
					"id": {"type": "string"},
					"namespace": {"type": "string"}
				}
			},
			"Settings": {
				"type": "object",
				"properties": {
					"document": {"$ref": "#/$defs/Document"},
					"primaryKey": {"type": "string"}
				}
			}
		},
		"$ref": "#/$defs/Settings"
	}`)

	storeSchema := []byte(`{
		"$defs": {
			"Document": {
				"type": "object",
				"properties": {
					"id": {"type": "string"}
				}
			},
			"StoreRequest": {
				"type": "object",
				"properties": {
					"document": {"$ref": "#/$defs/Document"},
					"operation": {"type": "string"}
				}
			}
		},
		"$ref": "#/$defs/StoreRequest"
	}`)

	queryResultSchema := []byte(`{
		"$defs": {
			"Document": {
				"type": "object",
				"properties": {
					"id": {"type": "string"}
				}
			},
			"QueryResultItem": {
				"type": "object",
				"properties": {
					"key": {"type": "string"},
					"document": {"$ref": "#/$defs/Document"}
				}
			},
			"QueryResult": {
				"type": "object",
				"properties": {
					"results": {
						"type": "array",
						"items": {"$ref": "#/$defs/QueryResultItem"}
					},
					"count": {"type": "integer"},
					"query": {"type": "string"}
				}
			}
		},
		"$ref": "#/$defs/QueryResult"
	}`)

	kvNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "kv",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:   v1alpha1.SettingsPort,
					Schema: settingsSchema,
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: v1alpha1.SettingsPort, Source: false, Schema: settingsSchema},
				{Name: "query_result", Source: true, Schema: queryResultSchema},
				{Name: "store", Source: false, Schema: storeSchema},
			},
		},
	}
	kvNode.Name = "kv-node"

	nodesMap := map[string]v1alpha1.TinyNode{
		"kv-node": kvNode,
	}

	// Run multiple times to catch non-determinism
	for i := 0; i < 10; i++ {
		result, err := SimulatePortData(ctx, nodesMap, "kv-node:query_result", nil)
		if err != nil {
			t.Fatalf("iteration %d: SimulatePortData() error = %v", i, err)
		}

		qr, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: expected map, got %T", i, result)
		}

		results, ok := qr["results"].([]interface{})
		if !ok || len(results) == 0 {
			t.Fatalf("iteration %d: expected non-empty results array, got %v", i, qr["results"])
		}

		item, ok := results[0].(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: expected map item, got %T", i, results[0])
		}

		doc, ok := item["document"].(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: expected document map, got %T (%v)", i, item["document"], item["document"])
		}

		// Document must have both properties from _settings schema
		if doc["id"] == nil {
			t.Errorf("iteration %d: document.id is nil, expected fake string value", i)
		}
		if doc["namespace"] == nil {
			t.Errorf("iteration %d: document.namespace is nil, expected fake string value — "+
				"settings definition was not propagated to source port", i)
		}

		// Values should be strings (fake data), not null
		if _, ok := doc["id"].(string); !ok {
			t.Errorf("iteration %d: document.id = %v (%T), expected string", i, doc["id"], doc["id"])
		}
		if _, ok := doc["namespace"].(string); !ok {
			t.Errorf("iteration %d: document.namespace = %v (%T), expected string", i, doc["namespace"], doc["namespace"])
		}
	}
}

// Helper to create ajson nodes for testing
func mustUnmarshal(s string) *ajson.Node {
	n, err := ajson.Unmarshal([]byte(s))
	if err != nil {
		panic(err)
	}
	return n
}
