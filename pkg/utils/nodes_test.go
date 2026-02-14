package utils

import (
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
)

func TestGetConfigurableDefinitions(t *testing.T) {
	tests := []struct {
		name         string
		node         v1alpha1.TinyNode
		from         *string
		wantCount    int
		wantContains []string
	}{
		{
			name: "empty node - no definitions",
			node: v1alpha1.TinyNode{
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{},
				},
			},
			from:      nil,
			wantCount: 0,
		},
		{
			name: "port with shared definition",
			node: v1alpha1.TinyNode{
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{
						{
							Name:   "out",
							Source: true,
							Schema: []byte(`{
								"$defs": {
									"SharedType": {"type": "string", "shared": true}
								}
							}`),
						},
					},
				},
			},
			from:         nil,
			wantCount:    1,
			wantContains: []string{"SharedType"},
		},
		{
			name: "settings port with configurable definition",
			node: v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Ports: []v1alpha1.TinyNodePortConfig{
						{
							Port: v1alpha1.SettingsPort,
							Schema: []byte(`{
								"$defs": {
									"ConfigurableField": {"type": "string", "configurable": true}
								}
							}`),
						},
					},
				},
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{
						{Name: v1alpha1.SettingsPort, Source: false},
					},
				},
			},
			from:         nil,
			wantCount:    1,
			wantContains: []string{"ConfigurableField"},
		},
		{
			name: "multiple ports with definitions",
			node: v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Ports: []v1alpha1.TinyNodePortConfig{
						{
							Port: v1alpha1.SettingsPort,
							Schema: []byte(`{
								"$defs": {
									"Config1": {"type": "string", "configurable": true}
								}
							}`),
						},
					},
				},
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{
						{
							Name:   "out",
							Source: true,
							Schema: []byte(`{
								"$defs": {
									"Shared1": {"type": "number", "shared": true}
								}
							}`),
						},
					},
				},
			},
			from:         nil,
			wantCount:    2,
			wantContains: []string{"Config1", "Shared1"},
		},
		{
			name: "filter by from parameter",
			node: v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Ports: []v1alpha1.TinyNodePortConfig{
						{
							Port: "in",
							From: "other-node:out",
							Schema: []byte(`{
								"$defs": {
									"FromOther": {"type": "string", "configurable": true}
								}
							}`),
						},
						{
							Port: "in",
							From: "my-node:out",
							Schema: []byte(`{
								"$defs": {
									"FromMy": {"type": "string", "configurable": true}
								}
							}`),
						},
					},
				},
			},
			from:         strPtr("my-node:out"),
			wantCount:    1,
			wantContains: []string{"FromMy"},
		},
		{
			name: "non-configurable definitions are ignored",
			node: v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Ports: []v1alpha1.TinyNodePortConfig{
						{
							Port: v1alpha1.SettingsPort,
							Schema: []byte(`{
								"$defs": {
									"NotConfigurable": {"type": "string", "configurable": false}
								}
							}`),
						},
					},
				},
			},
			from:      nil,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defs := GetConfigurableDefinitions(tt.node, tt.from)

			if len(defs) != tt.wantCount {
				t.Errorf("GetConfigurableDefinitions() returned %d definitions, want %d", len(defs), tt.wantCount)
			}

			for _, key := range tt.wantContains {
				if _, ok := defs[key]; !ok {
					t.Errorf("GetConfigurableDefinitions() missing expected key %q", key)
				}
			}
		})
	}
}

func TestGetFlowMaps(t *testing.T) {
	// Create a simple flow with two connected nodes
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
					Schema:        []byte(`{"type": "object", "properties": {"data": {"type": "string"}}}`),
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

	t.Run("returns all maps correctly", func(t *testing.T) {
		portStatusSchemaMap, portConfigMap, destinationsMap, portSchemaMap, targetPortsMap, err := GetFlowMaps(nodesMap)

		if err != nil {
			t.Fatalf("GetFlowMaps() error = %v", err)
		}

		// Check portStatusSchemaMap
		if _, ok := portStatusSchemaMap["source:out"]; !ok {
			t.Error("portStatusSchemaMap missing source:out")
		}
		if _, ok := portStatusSchemaMap["target:in"]; !ok {
			t.Error("portStatusSchemaMap missing target:in")
		}

		// Check portConfigMap
		if configs, ok := portConfigMap["target:in"]; !ok || len(configs) == 0 {
			t.Error("portConfigMap missing target:in config")
		}

		// Check destinationsMap
		if dests, ok := destinationsMap["target:in"]; !ok || len(dests) == 0 {
			t.Error("destinationsMap missing target:in destinations")
		} else {
			if dests[0].Name != "source:out" {
				t.Errorf("destinationsMap[target:in] = %v, want source:out", dests[0].Name)
			}
		}

		// Check portSchemaMap
		if _, ok := portSchemaMap["source:out"]; !ok {
			t.Error("portSchemaMap missing source:out")
		}

		// Check targetPortsMap
		if _, ok := targetPortsMap["target:in"]; !ok {
			t.Error("targetPortsMap missing target:in")
		}
		if _, ok := targetPortsMap["source:out"]; ok {
			t.Error("targetPortsMap should not contain source port source:out")
		}
	})

	t.Run("empty nodes map", func(t *testing.T) {
		emptyMap := map[string]v1alpha1.TinyNode{}
		_, _, _, _, _, err := GetFlowMaps(emptyMap)

		if err != nil {
			t.Errorf("GetFlowMaps() with empty map error = %v", err)
		}
	})
}

func TestApiNodeToMap(t *testing.T) {
	node := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "test-component",
			Module:    "test-module",
		},
		Status: v1alpha1.TinyNodeStatus{
			Module: v1alpha1.TinyNodeModuleStatus{
				Version: "1.0.0",
			},
			Component: v1alpha1.TinyNodeComponentStatus{
				Description: "Test Component",
				Info:        "Detailed info",
			},
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Label: "Input", Position: 0},
				{Name: "out", Source: true, Label: "Output", Position: 2},
			},
		},
	}
	node.Name = "test-node"
	node.Annotations = map[string]string{
		v1alpha1.ComponentPosXAnnotation:    "100",
		v1alpha1.ComponentPosYAnnotation:    "200",
		v1alpha1.ComponentPosSpinAnnotation: "1",
		v1alpha1.NodeLabelAnnotation:        "Custom Label",
	}
	node.Labels = map[string]string{
		v1alpha1.FlowNameLabel: "test-flow",
	}

	t.Run("full conversion", func(t *testing.T) {
		result := ApiNodeToMap(node, nil, false)

		// Check basic structure
		if result["type"] != "tinyNode" {
			t.Errorf("type = %v, want tinyNode", result["type"])
		}
		if result["id"] != "test-node" {
			t.Errorf("id = %v, want test-node", result["id"])
		}

		// Check position
		pos := result["position"].(map[string]interface{})
		if pos["x"] != 100 {
			t.Errorf("position.x = %v, want 100", pos["x"])
		}
		if pos["y"] != 200 {
			t.Errorf("position.y = %v, want 200", pos["y"])
		}

		// Check data
		data := result["data"].(map[string]interface{})
		if data["component"] != "test-component" {
			t.Errorf("data.component = %v, want test-component", data["component"])
		}
		if data["label"] != "Custom Label" {
			t.Errorf("data.label = %v, want Custom Label", data["label"])
		}
		if data["spin"] != 1 {
			t.Errorf("data.spin = %v, want 1", data["spin"])
		}
		if data["flow_id"] != "test-flow" {
			t.Errorf("data.flow_id = %v, want test-flow", data["flow_id"])
		}

		// Check handles
		handles := data["handles"].([]interface{})
		if len(handles) != 2 {
			t.Errorf("handles count = %d, want 2", len(handles))
		}
	})

	t.Run("minimal conversion", func(t *testing.T) {
		result := ApiNodeToMap(node, nil, true)

		data := result["data"].(map[string]interface{})
		handles := data["handles"].([]interface{})

		// In minimal mode, handles should not have schema/configuration
		for _, h := range handles {
			handle := h.(map[string]interface{})
			if _, hasSchema := handle["schema"]; hasSchema {
				t.Error("minimal mode should not include schema in handles")
			}
		}
	})

	t.Run("with extra data", func(t *testing.T) {
		extra := map[string]interface{}{
			"blocked": true,
			"stats":   map[string]interface{}{"count": 10},
		}
		result := ApiNodeToMap(node, extra, false)

		data := result["data"].(map[string]interface{})
		if data["blocked"] != true {
			t.Error("expected blocked = true from extra data")
		}
		if data["stats"] == nil {
			t.Error("expected stats from extra data")
		}
	})

	t.Run("default position when annotations missing", func(t *testing.T) {
		nodeNoPos := v1alpha1.TinyNode{
			Spec: v1alpha1.TinyNodeSpec{
				Component: "test",
			},
		}
		nodeNoPos.Name = "test"

		result := ApiNodeToMap(nodeNoPos, nil, true)
		pos := result["position"].(map[string]interface{})

		if pos["x"] != 600 {
			t.Errorf("default position.x = %v, want 600", pos["x"])
		}
		if pos["y"] != 200 {
			t.Errorf("default position.y = %v, want 200", pos["y"])
		}
	})
}

func TestApiEdgeToProtoMap(t *testing.T) {
	node := &v1alpha1.TinyNode{}
	node.Name = "source-node"

	tests := []struct {
		name    string
		edge    *v1alpha1.TinyNodeEdge
		data    map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid edge",
			edge: &v1alpha1.TinyNodeEdge{
				ID:   "edge-123",
				Port: "out",
				To:   "target-node:in",
			},
			data:    map[string]interface{}{"valid": true},
			wantErr: false,
		},
		{
			name: "invalid edge - no colon",
			edge: &v1alpha1.TinyNodeEdge{
				ID:   "edge-123",
				Port: "out",
				To:   "invalid-format",
			},
			data:    map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "invalid edge - too many colons",
			edge: &v1alpha1.TinyNodeEdge{
				ID:   "edge-123",
				Port: "out",
				To:   "a:b:c",
			},
			data:    map[string]interface{}{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ApiEdgeToProtoMap(node, tt.edge, tt.data)

			if (err != nil) != tt.wantErr {
				t.Errorf("ApiEdgeToProtoMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if result["id"] != tt.edge.ID {
					t.Errorf("id = %v, want %v", result["id"], tt.edge.ID)
				}
				if result["source"] != "source-node" {
					t.Errorf("source = %v, want source-node", result["source"])
				}
				if result["sourceHandle"] != tt.edge.Port {
					t.Errorf("sourceHandle = %v, want %v", result["sourceHandle"], tt.edge.Port)
				}
				if result["type"] != "tinyEdge" {
					t.Errorf("type = %v, want tinyEdge", result["type"])
				}
			}
		})
	}
}

func TestNodesToGraph(t *testing.T) {
	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "e1", Port: "out", To: "target:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true},
			},
		},
	}
	sourceNode.Name = "source"
	sourceNode.Labels = map[string]string{v1alpha1.FlowNameLabel: "flow1"}

	targetNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "target",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false},
			},
		},
	}
	targetNode.Name = "target"
	targetNode.Labels = map[string]string{v1alpha1.FlowNameLabel: "flow1"}

	nodesMap := map[string]v1alpha1.TinyNode{
		"source": sourceNode,
		"target": targetNode,
	}

	t.Run("all nodes without flow filter", func(t *testing.T) {
		nodes, edges, err := NodesToGraph(nodesMap, nil)

		if err != nil {
			t.Fatalf("error = %v", err)
		}

		if len(nodes) != 2 {
			t.Errorf("nodes count = %d, want 2", len(nodes))
		}

		if len(edges) != 1 {
			t.Errorf("edges count = %d, want 1", len(edges))
		}
	})

	t.Run("filter by flow", func(t *testing.T) {
		flowName := "flow1"
		nodes, edges, err := NodesToGraph(nodesMap, &flowName)

		if err != nil {
			t.Fatalf("error = %v", err)
		}

		if len(nodes) != 2 {
			t.Errorf("nodes count = %d, want 2", len(nodes))
		}

		if len(edges) != 1 {
			t.Errorf("edges count = %d, want 1", len(edges))
		}
	})

	t.Run("filter excludes other flow nodes", func(t *testing.T) {
		otherFlowName := "other-flow"
		nodes, edges, err := NodesToGraph(nodesMap, &otherFlowName)

		if err != nil {
			t.Fatalf("error = %v", err)
		}

		if len(nodes) != 0 {
			t.Errorf("nodes count = %d, want 0 (filtered out)", len(nodes))
		}

		if len(edges) != 0 {
			t.Errorf("edges count = %d, want 0 (filtered out)", len(edges))
		}
	})
}

func TestNodesToGraphWithSharedNodes(t *testing.T) {
	// Node belongs to flow1 but shared with flow2
	sharedNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "shared",
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true},
			},
		},
	}
	sharedNode.Name = "shared"
	sharedNode.Labels = map[string]string{v1alpha1.FlowNameLabel: "flow1"}
	sharedNode.Annotations = map[string]string{v1alpha1.SharedWithFlowsAnnotation: "flow2,flow3"}

	nodesMap := map[string]v1alpha1.TinyNode{
		"shared": sharedNode,
	}

	t.Run("node visible in its own flow", func(t *testing.T) {
		flowName := "flow1"
		nodes, _, err := NodesToGraph(nodesMap, &flowName)

		if err != nil {
			t.Fatalf("error = %v", err)
		}

		if len(nodes) != 1 {
			t.Errorf("nodes count = %d, want 1", len(nodes))
		}

		// Should not be blocked in its own flow
		nodeData := nodes[0].(map[string]interface{})["data"].(map[string]interface{})
		if nodeData["blocked"] == true {
			t.Error("node should not be blocked in its own flow")
		}
	})

	t.Run("node visible and blocked in shared flow", func(t *testing.T) {
		flowName := "flow2"
		nodes, _, err := NodesToGraph(nodesMap, &flowName)

		if err != nil {
			t.Fatalf("error = %v", err)
		}

		if len(nodes) != 1 {
			t.Errorf("nodes count = %d, want 1 (shared)", len(nodes))
		}

		// Should be blocked in shared flow
		nodeData := nodes[0].(map[string]interface{})["data"].(map[string]interface{})
		if nodeData["blocked"] != true {
			t.Error("node should be blocked in shared flow")
		}
	})

	t.Run("node not visible in unrelated flow", func(t *testing.T) {
		flowName := "unrelated-flow"
		nodes, _, err := NodesToGraph(nodesMap, &flowName)

		if err != nil {
			t.Fatalf("error = %v", err)
		}

		if len(nodes) != 0 {
			t.Errorf("nodes count = %d, want 0 (not shared)", len(nodes))
		}
	})
}

func TestGetPortFullName(t *testing.T) {
	tests := []struct {
		nodeName string
		portName string
		want     string
	}{
		{"node", "port", "node:port"},
		{"my-node-123", "out", "my-node-123:out"},
		{"node", "_settings", "node:_settings"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := GetPortFullName(tt.nodeName, tt.portName)
			if got != tt.want {
				t.Errorf("GetPortFullName(%q, %q) = %q, want %q",
					tt.nodeName, tt.portName, got, tt.want)
			}
		})
	}
}

// TestGetFlowMaps_DefinitionReplacementOrder verifies that source port definitions
// get the fully-replaced version from _settings, even when the source port name
// sorts alphabetically before the target port name (e.g. query_result before store).
//
// This was a bug: query_result was processed before store in the replacement loop,
// so it got the unreplaced store definition (with only {id}) instead of the
// _settings-replaced version (with {id, namespace}).
func TestGetFlowMaps_DefinitionReplacementOrder(t *testing.T) {
	// Simulate a KV-like component:
	// - _settings target port: Document with {id, namespace} (user configured)
	// - store target port: Document with {id} (default from Go struct)
	// - query_result source port: Document with {id} (default)
	//
	// After processing, query_result's Document $def should have {id, namespace}
	// because target ports are replaced first, then source ports read the updated definitions.

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
			// User-configured _settings with custom Document schema
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:   v1alpha1.SettingsPort,
					Schema: settingsSchema,
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				// Sorted alphabetically (as runner.go does)
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

	_, _, _, portSchemaMap, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		t.Fatalf("GetFlowMaps() error = %v", err)
	}

	// Check that query_result's Document definition got the _settings properties
	qrSchema, ok := portSchemaMap["kv-node:query_result"]
	if !ok {
		t.Fatal("portSchemaMap missing kv-node:query_result")
	}

	defs, err := qrSchema.GetKey("$defs")
	if err != nil {
		t.Fatalf("query_result schema has no $defs: %v", err)
	}

	docDef, err := defs.GetKey("Document")
	if err != nil {
		t.Fatalf("query_result $defs has no Document: %v", err)
	}

	props, err := docDef.GetKey("properties")
	if err != nil {
		t.Fatalf("Document has no properties: %v", err)
	}

	keys := props.Keys()
	hasID := false
	hasNamespace := false
	for _, k := range keys {
		if k == "id" {
			hasID = true
		}
		if k == "namespace" {
			hasNamespace = true
		}
	}

	if !hasID {
		t.Error("query_result Document definition missing 'id' property")
	}
	if !hasNamespace {
		t.Error("query_result Document definition missing 'namespace' property — " +
			"settings definition was not propagated to source port")
	}
}

// Helper function
func strPtr(s string) *string {
	return &s
}
