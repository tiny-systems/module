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

// TestSimulatePortData_HTTPServerLikePartialEdge simulates the real-world scenario
// where an HTTP server Start port has an array field (hostnames []string) that
// is NOT mapped in the incoming edge config. The generator should fill in
// unmapped properties with mock data.
func TestSimulatePortData_HTTPServerLikePartialEdge(t *testing.T) {
	ctx := context.Background()

	// Schema for the source port — a simple context object
	sourceOutSchema := []byte(`{
		"$defs": {
			"Context": {
				"type": "object",
				"properties": {
					"method": {"type": "string"},
					"url": {"type": "string"}
				}
			}
		},
		"$ref": "#/$defs/Context"
	}`)

	// Schema for http_server start port — mirrors real HTTP server Start struct
	// with autoHostName (bool), hostnames ([]string), readTimeout, writeTimeout (int),
	// and a context ($ref to Context)
	startSchema := []byte(`{
		"$defs": {
			"Start": {
				"type": "object",
				"properties": {
					"autoHostName": {"type": "boolean"},
					"hostnames": {
						"type": "array",
						"items": {"$ref": "#/$defs/String"}
					},
					"readTimeout": {"type": "integer"},
					"writeTimeout": {"type": "integer"},
					"context": {"$ref": "#/$defs/Context"}
				}
			},
			"Context": {
				"type": "object",
				"configurable": true,
				"properties": {
					"method": {"type": "string"},
					"url": {"type": "string"}
				}
			},
			"String": {
				"type": "string"
			}
		},
		"$ref": "#/$defs/Start"
	}`)

	// Source node with edge to http-server:start
	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "trigger",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "e1", Port: "out", To: "http-server:start"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: sourceOutSchema},
			},
		},
	}
	sourceNode.Name = "trigger"

	// HTTP server node with start target port
	// Edge config maps autoHostName, readTimeout, writeTimeout, context but NOT hostnames
	httpNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "http_server",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port: "start",
					From: "trigger:out",
					Configuration: []byte(`{
						"autoHostName": true,
						"readTimeout": 30,
						"writeTimeout": 30,
						"context": "{{$}}"
					}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "start", Source: false, Schema: startSchema},
			},
		},
	}
	httpNode.Name = "http-server"

	nodesMap := map[string]v1alpha1.TinyNode{
		"trigger":     sourceNode,
		"http-server": httpNode,
	}

	// Run multiple times to catch non-determinism
	for i := 0; i < 10; i++ {
		result, err := SimulatePortData(ctx, nodesMap, "http-server:start", nil)
		if err != nil {
			t.Fatalf("iteration %d: SimulatePortData() error = %v", i, err)
		}

		m, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: expected map, got %T", i, result)
		}

		// Edge-mapped fields must be present
		if _, ok := m["autoHostName"]; !ok {
			t.Errorf("iteration %d: autoHostName missing", i)
		}
		if _, ok := m["readTimeout"]; !ok {
			t.Errorf("iteration %d: readTimeout missing", i)
		}
		if _, ok := m["writeTimeout"]; !ok {
			t.Errorf("iteration %d: writeTimeout missing", i)
		}
		if _, ok := m["context"]; !ok {
			t.Errorf("iteration %d: context missing", i)
		}

		// Unmapped array field must be filled with mock data
		hostnames, ok := m["hostnames"]
		if !ok {
			t.Fatalf("iteration %d: hostnames missing — generator should fill in unmapped array properties", i)
		}
		arr, ok := hostnames.([]interface{})
		if !ok {
			t.Fatalf("iteration %d: hostnames should be array, got %T", i, hostnames)
		}
		if len(arr) == 0 {
			t.Errorf("iteration %d: hostnames array should have at least one mock element", i)
		}
	}
}

// TestSimulatePortData_MultipleEdgesToSamePort verifies that when multiple
// edges connect to the same target port, the simulation uses the first edge's data.
func TestSimulatePortData_MultipleEdgesToSamePort(t *testing.T) {
	ctx := context.Background()

	targetSchema := []byte(`{
		"$defs": {
			"Input": {
				"type": "object",
				"properties": {
					"value": {"type": "string"},
					"count": {"type": "integer"}
				}
			}
		},
		"$ref": "#/$defs/Input"
	}`)

	source1 := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source1",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "e1", Port: "out", To: "target:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"type": "object", "properties": {"data": {"type": "string"}}}`)},
			},
		},
	}
	source1.Name = "source1"

	source2 := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source2",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "e2", Port: "out", To: "target:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"type": "object", "properties": {"info": {"type": "string"}}}`)},
			},
		},
	}
	source2.Name = "source2"

	targetNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "target",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:          "in",
					From:          "source1:out",
					Configuration: []byte(`{"value": "{{$.data}}", "count": 1}`),
				},
				{
					Port:          "in",
					From:          "source2:out",
					Configuration: []byte(`{"value": "{{$.info}}", "count": 2}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Schema: targetSchema},
			},
		},
	}
	targetNode.Name = "target"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source1": source1,
		"source2": source2,
		"target":  targetNode,
	}

	result, err := SimulatePortData(ctx, nodesMap, "target:in", nil)
	if err != nil {
		t.Fatalf("SimulatePortData() error = %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}

	// Both fields must be present
	if _, ok := m["value"]; !ok {
		t.Error("value field missing")
	}
	if _, ok := m["count"]; !ok {
		t.Error("count field missing")
	}
}

// TestSimulatePortData_ArrayOfObjectsWithConfigurableRef verifies that arrays
// of objects referencing configurable definitions get proper mock data,
// including when those definitions are enriched by _settings.
func TestSimulatePortData_ArrayOfObjectsWithConfigurableRef(t *testing.T) {
	ctx := context.Background()

	// Settings schema — Document with configurable fields {id, name, email}
	settingsSchema := []byte(`{
		"$defs": {
			"Document": {
				"type": "object",
				"configurable": true,
				"properties": {
					"id": {"type": "string"},
					"name": {"type": "string"},
					"email": {"type": "string", "format": "email"}
				}
			},
			"Settings": {
				"type": "object",
				"properties": {
					"collection": {"type": "string"}
				}
			}
		},
		"$ref": "#/$defs/Settings"
	}`)

	// List output schema — array of Document items
	listSchema := []byte(`{
		"$defs": {
			"Document": {
				"type": "object",
				"properties": {
					"id": {"type": "string"}
				}
			},
			"ListResult": {
				"type": "object",
				"properties": {
					"items": {
						"type": "array",
						"items": {"$ref": "#/$defs/Document"}
					},
					"total": {"type": "integer"}
				}
			}
		},
		"$ref": "#/$defs/ListResult"
	}`)

	dbNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "database",
			Ports: []v1alpha1.TinyNodePortConfig{
				{Port: v1alpha1.SettingsPort, Schema: settingsSchema},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: v1alpha1.SettingsPort, Source: false, Schema: settingsSchema},
				{Name: "list", Source: true, Schema: listSchema},
			},
		},
	}
	dbNode.Name = "db"

	nodesMap := map[string]v1alpha1.TinyNode{
		"db": dbNode,
	}

	// Run multiple times for non-determinism
	for i := 0; i < 10; i++ {
		result, err := SimulatePortData(ctx, nodesMap, "db:list", nil)
		if err != nil {
			t.Fatalf("iteration %d: error = %v", i, err)
		}

		m, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: expected map, got %T", i, result)
		}

		// items must be a non-empty array
		items, ok := m["items"].([]interface{})
		if !ok || len(items) == 0 {
			t.Fatalf("iteration %d: items should be non-empty array, got %v", i, m["items"])
		}

		// Each item must have all Document properties from _settings enrichment
		doc, ok := items[0].(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: item should be map, got %T", i, items[0])
		}

		for _, field := range []string{"id", "name", "email"} {
			if doc[field] == nil {
				t.Errorf("iteration %d: Document.%s is nil — settings enrichment should propagate all fields", i, field)
			}
		}

		// total must be present
		if _, ok := m["total"]; !ok {
			t.Errorf("iteration %d: total field missing", i)
		}
	}
}

// TestSimulatePortData_NoEdgesPureMockData verifies that a target port
// with no incoming edges gets pure mock data with all fields populated.
func TestSimulatePortData_NoEdgesPureMockData(t *testing.T) {
	ctx := context.Background()

	schema := []byte(`{
		"$defs": {
			"Config": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"count": {"type": "integer"},
					"tags": {
						"type": "array",
						"items": {"type": "string"}
					},
					"enabled": {"type": "boolean"},
					"nested": {
						"type": "object",
						"properties": {
							"key": {"type": "string"},
							"value": {"type": "number"}
						}
					}
				}
			}
		},
		"$ref": "#/$defs/Config"
	}`)

	node := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "standalone",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "config", Source: false, Schema: schema},
			},
		},
	}
	node.Name = "standalone"

	nodesMap := map[string]v1alpha1.TinyNode{
		"standalone": node,
	}

	result, err := SimulatePortData(ctx, nodesMap, "standalone:config", nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}

	// All top-level properties must be present
	for _, field := range []string{"name", "count", "tags", "enabled", "nested"} {
		if _, ok := m[field]; !ok {
			t.Errorf("field %q missing from pure mock data", field)
		}
	}

	// tags should be array with at least one element
	tags, ok := m["tags"].([]interface{})
	if !ok || len(tags) == 0 {
		t.Errorf("tags should be non-empty array, got %v", m["tags"])
	}

	// nested should be object with sub-fields
	nested, ok := m["nested"].(map[string]interface{})
	if !ok {
		t.Errorf("nested should be map, got %T", m["nested"])
	} else {
		if _, ok := nested["key"]; !ok {
			t.Error("nested.key missing")
		}
		if _, ok := nested["value"]; !ok {
			t.Error("nested.value missing")
		}
	}
}

// TestSimulatePortData_SharedDefinitionConsistency verifies that when a definition
// (like String) appears in multiple ports, the simulation produces consistent
// results across multiple runs.
func TestSimulatePortData_SharedDefinitionConsistency(t *testing.T) {
	ctx := context.Background()

	// Two ports sharing "String" definition name
	inSchema := []byte(`{
		"$defs": {
			"String": {"type": "string"},
			"Request": {
				"type": "object",
				"properties": {
					"query": {"$ref": "#/$defs/String"},
					"limit": {"type": "integer"}
				}
			}
		},
		"$ref": "#/$defs/Request"
	}`)

	outSchema := []byte(`{
		"$defs": {
			"String": {"type": "string"},
			"Response": {
				"type": "object",
				"properties": {
					"result": {"$ref": "#/$defs/String"},
					"items": {
						"type": "array",
						"items": {"$ref": "#/$defs/String"}
					}
				}
			}
		},
		"$ref": "#/$defs/Response"
	}`)

	node := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "search",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Schema: inSchema},
				{Name: "out", Source: true, Schema: outSchema},
			},
		},
	}
	node.Name = "search"

	nodesMap := map[string]v1alpha1.TinyNode{
		"search": node,
	}

	// Verify both ports produce valid data across multiple runs
	for i := 0; i < 10; i++ {
		// Test target port
		inResult, err := SimulatePortData(ctx, nodesMap, "search:in", nil)
		if err != nil {
			t.Fatalf("iteration %d: in port error = %v", i, err)
		}
		inMap, ok := inResult.(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: in result should be map, got %T", i, inResult)
		}
		if _, ok := inMap["query"]; !ok {
			t.Errorf("iteration %d: in.query missing", i)
		}
		if _, ok := inMap["limit"]; !ok {
			t.Errorf("iteration %d: in.limit missing", i)
		}

		// Test source port
		outResult, err := SimulatePortData(ctx, nodesMap, "search:out", nil)
		if err != nil {
			t.Fatalf("iteration %d: out port error = %v", i, err)
		}
		outMap, ok := outResult.(map[string]interface{})
		if !ok {
			t.Fatalf("iteration %d: out result should be map, got %T", i, outResult)
		}
		if _, ok := outMap["result"]; !ok {
			t.Errorf("iteration %d: out.result missing", i)
		}
		items, ok := outMap["items"].([]interface{})
		if !ok || len(items) == 0 {
			t.Errorf("iteration %d: out.items should be non-empty array, got %v", i, outMap["items"])
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
