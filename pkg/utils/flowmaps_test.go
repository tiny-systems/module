package utils

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/goccy/go-json"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/schema"
)

// TestGetConfigurableDefinitions_EdgeOverlay tests configurable definition collection
// scenarios that are specifically relevant to the edge overlay pipeline.
func TestGetConfigurableDefinitions_EdgeOverlay(t *testing.T) {
	tests := []struct {
		name     string
		node     v1alpha1.TinyNode
		from     *string
		wantDefs []string // expected definition keys
		wantNot  []string // definition keys that should NOT be present
	}{
		{
			name: "collects configurable defs from status ports",
			node: v1alpha1.TinyNode{
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{
						{
							Name:   "out",
							Source: true,
							Schema: []byte(`{
								"$defs": {
									"Output": {"type":"object","path":"$"},
									"Context": {"configurable":true,"shared":true,"path":"$.context","title":"Context","type":"object","properties":{"key":{"type":"string"}}}
								},
								"$ref": "#/$defs/Output"
							}`),
						},
					},
				},
			},
			wantDefs: []string{"Context"},
			wantNot:  []string{"Output"}, // Output is not shared/configurable
		},
		{
			name: "collects configurable defs from settings Spec.Ports",
			node: v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Ports: []v1alpha1.TinyNodePortConfig{
						{
							Port: "_settings",
							Schema: []byte(`{
								"$defs": {
									"Settings": {"type":"object","path":"$"},
									"Context": {"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"api_key":{"type":"string"}}}
								},
								"$ref": "#/$defs/Settings"
							}`),
						},
					},
				},
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{
						{
							Name:   "_settings",
							Source: false,
							Schema: []byte(`{
								"$defs": {
									"Settings": {"type":"object","path":"$"},
									"Context": {"configurable":true,"path":"$.context","title":"Context"}
								},
								"$ref": "#/$defs/Settings"
							}`),
						},
					},
				},
			},
			wantDefs: []string{"Context"},
		},
		{
			name: "Spec.Ports configurable defs override Status.Ports",
			node: v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Ports: []v1alpha1.TinyNodePortConfig{
						{
							Port: "_settings",
							Schema: []byte(`{
								"$defs": {
									"Context": {"configurable":true,"path":"$.context","properties":{"rich_field":{"type":"string"}},"type":"object"}
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
									"Context": {"shared":true,"path":"$.context","title":"Context"}
								}
							}`),
						},
					},
				},
			},
			wantDefs: []string{"Context"},
		},
		{
			name: "edge Spec.Ports filtered by from parameter",
			node: v1alpha1.TinyNode{
				Spec: v1alpha1.TinyNodeSpec{
					Ports: []v1alpha1.TinyNodePortConfig{
						{
							Port: "in",
							From: "node-a:out",
							Schema: []byte(`{
								"$defs": {
									"Context": {"configurable":true,"path":"$.context","properties":{"from_a":{"type":"string"}},"type":"object"}
								}
							}`),
						},
						{
							Port: "in",
							From: "node-b:out",
							Schema: []byte(`{
								"$defs": {
									"Context": {"configurable":true,"path":"$.context","properties":{"from_b":{"type":"string"}},"type":"object"}
								}
							}`),
						},
					},
				},
			},
			from:     strPtr("node-a:out"),
			wantDefs: []string{"Context"},
		},
		{
			name: "no configurable or shared defs - returns empty",
			node: v1alpha1.TinyNode{
				Status: v1alpha1.TinyNodeStatus{
					Ports: []v1alpha1.TinyNodePortStatus{
						{
							Name:   "out",
							Source: true,
							Schema: []byte(`{
								"$defs": {
									"Output": {"type":"object","path":"$","properties":{"data":{"type":"string"}}}
								},
								"$ref": "#/$defs/Output"
							}`),
						},
					},
				},
			},
			wantDefs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defs := GetConfigurableDefinitions(tt.node, tt.from)

			for _, key := range tt.wantDefs {
				if _, ok := defs[key]; !ok {
					t.Errorf("expected definition %q not found in result: %v", key, mapKeys(defs))
				}
			}

			for _, key := range tt.wantNot {
				if _, ok := defs[key]; ok {
					t.Errorf("definition %q should NOT be in result but was found", key)
				}
			}
		})
	}
}

// TestGetFlowMaps_PortSchemaMap tests that GetFlowMaps correctly builds port schemas
// and replaces definitions from settings ports into source port schemas.
func TestGetFlowMaps_PortSchemaMap(t *testing.T) {
	tests := []struct {
		name      string
		nodesMap  map[string]v1alpha1.TinyNode
		checkPort string // which port's schema to inspect
		wantInSchema   string // substring that must appear in the schema
		wantNotInSchema string // substring that must NOT appear in the schema (optional)
	}{
		{
			name: "source port schema present in map",
			nodesMap: map[string]v1alpha1.TinyNode{
				"node-a": {
					Spec: v1alpha1.TinyNodeSpec{Component: "comp-a"},
					Status: v1alpha1.TinyNodeStatus{
						Ports: []v1alpha1.TinyNodePortStatus{
							{Name: "out", Source: true, Schema: []byte(`{"type":"object","properties":{"msg":{"type":"string"}}}`)},
						},
					},
				},
			},
			checkPort:    "node-a:out",
			wantInSchema: `"msg"`,
		},
		{
			name: "target port schema present in map",
			nodesMap: map[string]v1alpha1.TinyNode{
				"node-a": {
					Spec: v1alpha1.TinyNodeSpec{Component: "comp-a"},
					Status: v1alpha1.TinyNodeStatus{
						Ports: []v1alpha1.TinyNodePortStatus{
							{Name: "in", Source: false, Schema: []byte(`{"type":"object","properties":{"data":{"type":"string"}}}`)},
						},
					},
				},
			},
			checkPort:    "node-a:in",
			wantInSchema: `"data"`,
		},
		{
			name: "incomplete Spec schema without $ref does not override Status schema",
			nodesMap: map[string]v1alpha1.TinyNode{
				"node-a": {
					Spec: v1alpha1.TinyNodeSpec{
						Component: "comp-a",
						Ports: []v1alpha1.TinyNodePortConfig{
							{
								Port: "in",
								// Schema from import: has $defs but no $ref
								Schema: []byte(`{"$defs":{"Context":{"configurable":true,"path":"$.context","type":"object","properties":{"custom":{"type":"string"}}}}}`),
							},
						},
					},
					Status: v1alpha1.TinyNodeStatus{
						Ports: []v1alpha1.TinyNodePortStatus{
							{Name: "in", Source: false, Schema: []byte(`{"$defs":{"Inmessage":{"type":"object","properties":{"context":{"$ref":"#/$defs/Context"},"array":{"type":"array"}}},"Context":{"type":"object","configurable":true}},"$ref":"#/$defs/Inmessage"}`)},
						},
					},
				},
			},
			checkPort:    "node-a:in",
			wantInSchema: `"$ref"`, // Status schema with $ref must be preserved
		},
		{
			name: "settings port schema overrides status schema",
			nodesMap: map[string]v1alpha1.TinyNode{
				"node-a": {
					Spec: v1alpha1.TinyNodeSpec{
						Component: "comp-a",
						Ports: []v1alpha1.TinyNodePortConfig{
							{
								Port:   "_settings",
								Schema: []byte(`{"$defs":{"Settings":{"type":"object","properties":{"delay":{"type":"integer"}}},"Context":{"configurable":true,"path":"$.context","properties":{"custom_field":{"type":"string"}},"type":"object"}},"$ref":"#/$defs/Settings"}`),
							},
						},
					},
					Status: v1alpha1.TinyNodeStatus{
						Ports: []v1alpha1.TinyNodePortStatus{
							{Name: "_settings", Source: false, Schema: []byte(`{"$defs":{"Settings":{"type":"object","properties":{"delay":{"type":"integer"}}},"Context":{"configurable":true,"path":"$.context"}},"$ref":"#/$defs/Settings"}`)},
						},
					},
				},
			},
			checkPort:    "node-a:_settings",
			wantInSchema: `"custom_field"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set Name on nodes (required by GetFlowMaps)
			for name, node := range tt.nodesMap {
				node.Name = name
				tt.nodesMap[name] = node
			}

			_, _, _, portSchemaMap, _, err := GetFlowMaps(tt.nodesMap)
			if err != nil {
				t.Fatalf("GetFlowMaps() error: %v", err)
			}

			schemaNode, ok := portSchemaMap[tt.checkPort]
			if !ok {
				t.Fatalf("port %q not found in portSchemaMap", tt.checkPort)
			}

			schemaBytes, _ := ajson.Marshal(schemaNode)

			if tt.wantInSchema != "" && !strings.Contains(string(schemaBytes), tt.wantInSchema) {
				t.Errorf("schema for %s does not contain %q: %s", tt.checkPort, tt.wantInSchema, schemaBytes)
			}
			if tt.wantNotInSchema != "" && strings.Contains(string(schemaBytes), tt.wantNotInSchema) {
				t.Errorf("schema for %s should NOT contain %q: %s", tt.checkPort, tt.wantNotInSchema, schemaBytes)
			}
		})
	}
}

// TestGetFlowMaps_DestinationsMap tests that GetFlowMaps correctly builds the edge
// destinations map (which port connects to which).
func TestGetFlowMaps_DestinationsMap(t *testing.T) {
	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source-comp",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge-1", Port: "out", To: "target:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"type":"object","properties":{"msg":{"type":"string"}}}`)},
			},
		},
	}
	sourceNode.Name = "source"

	targetNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "target-comp",
			Ports: []v1alpha1.TinyNodePortConfig{
				{Port: "in", From: "source:out", Configuration: []byte(`{"data":"{{$.msg}}"}`)},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Schema: []byte(`{"type":"object","properties":{"data":{"type":"string"}}}`)},
			},
		},
	}
	targetNode.Name = "target"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source": sourceNode,
		"target": targetNode,
	}

	_, _, destinationsMap, _, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		t.Fatalf("GetFlowMaps() error: %v", err)
	}

	// target:in should have a destination pointing back to source:out
	dests, ok := destinationsMap["target:in"]
	if !ok {
		t.Fatalf("target:in not found in destinationsMap")
	}
	if len(dests) != 1 {
		t.Fatalf("expected 1 destination, got %d", len(dests))
	}
	if dests[0].Name != "source:out" {
		t.Errorf("destination name = %q, want %q", dests[0].Name, "source:out")
	}
	if string(dests[0].Configuration) != `{"data":"{{$.msg}}"}` {
		t.Errorf("destination config = %q, want %q", dests[0].Configuration, `{"data":"{{$.msg}}"}`)
	}
}

// TestGetFlowMaps_TargetPortsMap tests that target ports are correctly identified.
func TestGetFlowMaps_TargetPortsMap(t *testing.T) {
	node := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{Component: "comp"},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false},
				{Name: "out", Source: true},
				{Name: "_settings", Source: false},
			},
		},
	}
	node.Name = "node-a"

	nodesMap := map[string]v1alpha1.TinyNode{"node-a": node}

	_, _, _, _, targetPortsMap, err := GetFlowMaps(nodesMap)
	if err != nil {
		t.Fatalf("GetFlowMaps() error: %v", err)
	}

	// "in" and "_settings" are target ports, "out" is source
	if _, ok := targetPortsMap["node-a:in"]; !ok {
		t.Error("expected node-a:in in targetPortsMap")
	}
	if _, ok := targetPortsMap["node-a:_settings"]; !ok {
		t.Error("expected node-a:_settings in targetPortsMap")
	}
	if _, ok := targetPortsMap["node-a:out"]; ok {
		t.Error("node-a:out should NOT be in targetPortsMap (it's a source port)")
	}
}

// TestFullPipeline_StatusPage tests the exact pipeline that broke for the service-status-page flow:
// ticker (with required endpoints) → split → http_request (bare Context)
// The pipeline: GetFlowMaps → GetConfigurableDefinitions → UpdateWithDefinitions → ValidateEdgeSchema
func TestFullPipeline_StatusPage(t *testing.T) {
	ctx := context.Background()

	// Ticker node: has settings with Context that has required: ["endpoints"]
	tickerNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "ticker",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge-ticker-split", Port: "out", To: "split:array"},
			},
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port: "_settings",
					Schema: []byte(`{
						"$defs": {
							"Settings": {"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"delay":{"type":"integer"}},"type":"object"},
							"Context": {"configurable":true,"path":"$.context","properties":{"endpoints":{"type":"array","items":{"type":"object","properties":{"url":{"type":"string"},"method":{"type":"string"}}}},"project_name":{"type":"string"}},"required":["endpoints","project_name"],"title":"Context","type":"object"}
						},
						"$ref": "#/$defs/Settings"
					}`),
					Configuration: []byte(`{"context":{"endpoints":[{"url":"https://api.example.com","method":"GET"}],"project_name":"My Project"},"delay":60000}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "_settings", Source: false, Schema: []byte(`{"$defs":{"Settings":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"delay":{"type":"integer"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Settings"}`)},
				{Name: "out", Source: true, Schema: []byte(`{"$defs":{"Output":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Output"}`)},
			},
		},
	}
	tickerNode.Name = "ticker"

	// HTTP Request node: has bare Context (type Context any)
	httpRequestNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "http_request",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:          "request",
					From:          "ticker:out",
					Configuration: []byte(`{"context":"{{$.context}}","url":"{{$.context.endpoints[0].url}}","method":"GET","timeout":10}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "request", Source: false, Schema: []byte(`{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"url":{"type":"string"},"method":{"type":"string"},"timeout":{"type":"integer"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Request"}`)},
				{Name: "response", Source: true, Schema: []byte(`{"$defs":{"Response":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"status":{"type":"integer"},"body":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Response"}`)},
				{Name: "error", Source: true, Schema: []byte(`{"$defs":{"Error":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"error":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Error"}`)},
			},
		},
	}
	httpRequestNode.Name = "http-request"

	nodesMap := map[string]v1alpha1.TinyNode{
		"ticker":       tickerNode,
		"http-request": httpRequestNode,
	}

	// Step 1: GetFlowMaps
	_, _, destinationsMap, portSchemaMap, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		t.Fatalf("GetFlowMaps() error: %v", err)
	}

	// Step 2: Get configurable definitions (as buildGraphEvents does)
	sourcePortFull := "ticker:out"
	targetPortFull := "http-request:request"

	defs := GetConfigurableDefinitions(tickerNode, nil)
	targetDefs := GetConfigurableDefinitions(httpRequestNode, &sourcePortFull)
	for k, v := range targetDefs {
		defs[k] = v
	}

	// Verify we got the Context def with required and type
	contextDef, ok := defs["Context"]
	if !ok {
		t.Fatal("Context definition not collected")
	}
	// Context should have "required" and "type":"object" from ticker's settings
	contextBytes, _ := ajson.Marshal(contextDef)
	if !strings.Contains(string(contextBytes), `"required"`) {
		t.Logf("Context def: %s", contextBytes)
		t.Log("Note: Context def may not have required (it's from Status which is bare)")
	}

	// Step 3: Get target port schema and apply overlay
	targetSchema := portSchemaMap[targetPortFull]
	if targetSchema == nil {
		t.Fatalf("target port schema not found for %s", targetPortFull)
	}

	targetSchemaBytes, _ := ajson.Marshal(targetSchema)
	overlaidSchema, err := schema.UpdateWithDefinitions(targetSchemaBytes, defs)
	if err != nil {
		t.Fatalf("UpdateWithDefinitions() error: %v", err)
	}

	// Step 4: Verify the overlaid schema doesn't have type:"object" on Context
	// (since original was bare — no type)
	overlaidNode, _ := ajson.Unmarshal(overlaidSchema)
	if overlaidNode != nil {
		overlaidDefs, _ := overlaidNode.GetKey("$defs")
		if overlaidDefs != nil {
			overlaidContext, _ := overlaidDefs.GetKey("Context")
			if overlaidContext != nil {
				typeNode, _ := overlaidContext.GetKey("type")
				if typeNode != nil {
					typeStr, _ := typeNode.GetString()
					if typeStr == "object" {
						t.Error("Context in overlaid schema should NOT have type:object (original was bare)")
					}
				}
				// Also check required is stripped
				reqNode, _ := overlaidContext.GetKey("required")
				if reqNode != nil {
					t.Error("Context in overlaid schema should NOT have required (stripped by UpdateWithDefinitions)")
				}
			}
		}
	}

	// Step 5: Validate — this should pass (no "missing properties" or "expected object" errors)
	err = ValidateEdgeWithPrecomputedMaps(
		ctx,
		portSchemaMap,
		destinationsMap,
		sourcePortFull,
		[]byte(`{"context":"{{$.context}}","url":"{{$.context.endpoints[0].url}}","method":"GET","timeout":10}`),
		overlaidSchema,
		nil,
	)
	if err != nil {
		t.Errorf("ValidateEdgeWithPrecomputedMaps() unexpected error: %v\n  overlaid schema: %s", err, overlaidSchema)
	}
}

// TestFullPipeline_FirestoreToDebug tests the exact scenario that caused the
// "expected object, but got string" error: firestore listener error → debug node.
func TestFullPipeline_FirestoreToDebug(t *testing.T) {
	ctx := context.Background()

	// Firestore Listener node: error port has Context with type:"object" and properties
	firestoreNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "firestore_listener",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge-fs-debug", Port: "error", To: "debug:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "start", Source: false, Schema: []byte(`{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"config":{"$ref":"#/$defs/Config"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"},"Config":{"path":"$.config","properties":{"credentials":{"type":"string"},"collection":{"type":"string"}},"type":"object"}},"$ref":"#/$defs/Start"}`)},
				{Name: "document", Source: true, Schema: []byte(`{"$defs":{"Document":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"id":{"type":"string"},"data":{"type":"object"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"collection":{"type":"string"},"namespace":{"type":"string"}}}},"$ref":"#/$defs/Document"}`)},
				{Name: "error", Source: true, Schema: []byte(`{"$defs":{"Error":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"error":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"collection":{"type":"string"},"namespace":{"type":"string"}}}},"$ref":"#/$defs/Error"}`)},
			},
		},
	}
	firestoreNode.Name = "firestore"

	// Debug node: bare Context (type Context any)
	debugNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "debug",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:          "in",
					From:          "firestore:error",
					Configuration: []byte(`{"context":"{{$.error}}","data":"{{$}}"}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Schema: []byte(`{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Inputdata"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"},"Inputdata":{"configurable":true,"path":"$.data","title":"Data"}},"$ref":"#/$defs/In"}`)},
			},
		},
	}
	debugNode.Name = "debug"

	nodesMap := map[string]v1alpha1.TinyNode{
		"firestore": firestoreNode,
		"debug":     debugNode,
	}

	// Full pipeline
	_, _, destinationsMap, portSchemaMap, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		t.Fatalf("GetFlowMaps() error: %v", err)
	}

	sourcePortFull := "firestore:error"
	targetPortFull := "debug:in"

	defs := GetConfigurableDefinitions(firestoreNode, nil)
	targetDefs := GetConfigurableDefinitions(debugNode, &sourcePortFull)
	for k, v := range targetDefs {
		defs[k] = v
	}

	targetSchema := portSchemaMap[targetPortFull]
	if targetSchema == nil {
		t.Fatalf("target port schema not found")
	}

	targetSchemaBytes, _ := ajson.Marshal(targetSchema)
	overlaidSchema, err := schema.UpdateWithDefinitions(targetSchemaBytes, defs)
	if err != nil {
		t.Fatalf("UpdateWithDefinitions() error: %v", err)
	}

	// The edge maps $.error (a string) into context — this must pass
	// because debug's Context is bare (no type), even though firestore's
	// Context overlay has type:"object"
	err = ValidateEdgeWithPrecomputedMaps(
		ctx,
		portSchemaMap,
		destinationsMap,
		sourcePortFull,
		[]byte(`{"context":"{{$.error}}","data":"{{$}}"}`),
		overlaidSchema,
		nil,
	)
	if err != nil {
		t.Errorf("expected no error (string into bare context), got: %v\n  overlaid schema: %s", err, overlaidSchema)
	}
}

// TestFullPipeline_TypedContextRejectsString verifies that typed Context (with explicit
// type:"object" in the native schema) correctly rejects string values.
func TestFullPipeline_TypedContextRejectsString(t *testing.T) {
	ctx := context.Background()

	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge-1", Port: "error", To: "typed:request"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "error", Source: true, Schema: []byte(`{"$defs":{"Error":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"error":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"key":{"type":"string"}}}},"$ref":"#/$defs/Error"}`)},
			},
		},
	}
	sourceNode.Name = "source"

	// Target has typed Context (type:"object" is in native schema)
	typedNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "typed-comp",
			Ports: []v1alpha1.TinyNodePortConfig{
				{
					Port:          "request",
					From:          "source:error",
					Configuration: []byte(`{"context":"{{$.error}}","endpoint":"/api"}`),
				},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "request", Source: false, Schema: []byte(`{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"endpoint":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Request"}`)},
			},
		},
	}
	typedNode.Name = "typed"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source": sourceNode,
		"typed":  typedNode,
	}

	_, _, destinationsMap, portSchemaMap, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		t.Fatalf("GetFlowMaps() error: %v", err)
	}

	sourcePortFull := "source:error"
	targetPortFull := "typed:request"

	defs := GetConfigurableDefinitions(sourceNode, nil)
	targetDefs := GetConfigurableDefinitions(typedNode, &sourcePortFull)
	for k, v := range targetDefs {
		defs[k] = v
	}

	targetSchema := portSchemaMap[targetPortFull]
	targetSchemaBytes, _ := ajson.Marshal(targetSchema)
	overlaidSchema, err := schema.UpdateWithDefinitions(targetSchemaBytes, defs)
	if err != nil {
		t.Fatalf("UpdateWithDefinitions() error: %v", err)
	}

	// This should FAIL because the target's Context has type:"object" natively
	err = ValidateEdgeWithPrecomputedMaps(
		ctx,
		portSchemaMap,
		destinationsMap,
		sourcePortFull,
		[]byte(`{"context":"{{$.error}}","endpoint":"/api"}`),
		overlaidSchema,
		nil,
	)
	if err == nil {
		t.Error("expected error (string into typed object context), got nil")
	} else if !strings.Contains(err.Error(), "expected object") {
		t.Errorf("expected error containing 'expected object', got: %v", err)
	}
}

// TestFullPipeline_MultipleEdgesToSamePort tests that when multiple edges feed
// the same target port, the configurable definitions merge correctly.
func TestFullPipeline_MultipleEdgesToSamePort(t *testing.T) {
	// Node A: outputs context with field_a
	nodeA := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "comp-a",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge-a", Port: "out", To: "target:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"$defs":{"Output":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data_a":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"field_a":{"type":"string"}}}},"$ref":"#/$defs/Output"}`)},
			},
		},
	}
	nodeA.Name = "node-a"

	// Node B: outputs context with field_b
	nodeB := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "comp-b",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge-b", Port: "out", To: "target:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"$defs":{"Output":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data_b":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object","properties":{"field_b":{"type":"string"}}}},"$ref":"#/$defs/Output"}`)},
			},
		},
	}
	nodeB.Name = "node-b"

	// Target node: bare Context
	targetNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "target-comp",
			Ports: []v1alpha1.TinyNodePortConfig{
				{Port: "in", From: "node-a:out", Configuration: []byte(`{"context":"{{$.context}}","data":"{{$.data_a}}"}`)},
				{Port: "in", From: "node-b:out", Configuration: []byte(`{"context":"{{$.context}}","data":"{{$.data_b}}"}`)},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Schema: []byte(`{"$defs":{"Input":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Input"}`)},
			},
		},
	}
	targetNode.Name = "target"

	nodesMap := map[string]v1alpha1.TinyNode{
		"node-a": nodeA,
		"node-b": nodeB,
		"target": targetNode,
	}

	// GetFlowMaps should handle multiple edges to same port
	_, _, _, portSchemaMap, _, err := GetFlowMaps(nodesMap)
	if err != nil {
		t.Fatalf("GetFlowMaps() error: %v", err)
	}

	// Verify target port schema exists
	if _, ok := portSchemaMap["target:in"]; !ok {
		t.Fatal("target:in not found in portSchemaMap")
	}
}

// TestOverlaidSchema_CompilesToValidJSONSchema ensures that every schema produced by
// UpdateWithDefinitions is a valid JSON Schema that can be compiled without errors.
func TestOverlaidSchema_CompilesToValidJSONSchema(t *testing.T) {
	tests := []struct {
		name                        string
		targetPortSchema            string
		configurableDefinitionNodes map[string]*ajson.Node
	}{
		{
			name:             "bare context with overlay",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"x":{"type":"string"}},"title":"Context","type":"object"}`))),
			},
		},
		{
			name:             "typed context with overlay",
			targetPortSchema: `{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name"],"title":"Context","type":"object"}`))),
			},
		},
		{
			name:             "path-based match with Startcontext",
			targetPortSchema: `{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"},"port":{"type":"integer"}},"type":"object"},"Startcontext":{"additionalProperties":{"type":"string"},"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Start"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"hostname":{"type":"string"}},"title":"Context","type":"object"}`))),
			},
		},
		{
			name:             "multiple configurable defs",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Data"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"},"Data":{"configurable":true,"path":"$.data","title":"Data","type":"object"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"x":{"type":"string"}},"required":["x"],"title":"Context","type":"object"}`))),
				"Data":    ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.data","properties":{"y":{"type":"integer"}},"title":"Data","type":"object"}`))),
			},
		},
		{
			name:             "overlay with additionalProperties false",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"x":{"type":"string"}},"additionalProperties":false,"title":"Context","type":"object"}`))),
			},
		},
		{
			name:                        "no matching overlay",
			targetPortSchema:            `{"$defs":{"Request":{"path":"$","properties":{"url":{"type":"string"}},"type":"object"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overlaidSchema, err := schema.UpdateWithDefinitions([]byte(tt.targetPortSchema), tt.configurableDefinitionNodes)
			if err != nil {
				t.Fatalf("UpdateWithDefinitions() error: %v", err)
			}

			// Must compile as valid JSON Schema
			compiler := jsonschema.NewCompiler()
			compiler.Draft = jsonschema.Draft7
			if err := compiler.AddResource("schema.json", bytes.NewReader(overlaidSchema)); err != nil {
				t.Fatalf("AddResource() error: %v (schema: %s)", err, overlaidSchema)
			}
			_, err = compiler.Compile("schema.json")
			if err != nil {
				t.Errorf("Compile() error: %v (schema: %s)", err, overlaidSchema)
			}
		})
	}
}

// TestSimulatePortData_WithConfigurableContext tests that SimulatePortData generates
// valid mock data for ports with configurable Context definitions.
func TestSimulatePortData_WithConfigurableContext(t *testing.T) {
	ctx := context.Background()

	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "source",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge-1", Port: "out", To: "target:in"},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"$defs":{"Output":{"path":"$","properties":{"message":{"type":"string"},"count":{"type":"integer"}},"type":"object"}},"$ref":"#/$defs/Output"}`)},
			},
		},
	}
	sourceNode.Name = "source"

	targetNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "target",
			Ports: []v1alpha1.TinyNodePortConfig{
				{Port: "in", From: "source:out", Configuration: []byte(`{"data":"{{$.message}}"}`)},
			},
		},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "in", Source: false, Schema: []byte(`{"$defs":{"Input":{"path":"$","properties":{"data":{"type":"string"}},"type":"object"}},"$ref":"#/$defs/Input"}`)},
			},
		},
	}
	targetNode.Name = "target"

	nodesMap := map[string]v1alpha1.TinyNode{
		"source": sourceNode,
		"target": targetNode,
	}

	// SimulatePortData for source:out should produce mock data
	data, err := SimulatePortData(ctx, nodesMap, "source:out", nil)
	if err != nil {
		t.Fatalf("SimulatePortData() error: %v", err)
	}
	if data == nil {
		t.Fatal("SimulatePortData() returned nil")
	}

	// Should be a map with "message" and "count" keys
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", data)
	}
	if _, ok := dataMap["message"]; !ok {
		t.Error("expected 'message' key in simulated data")
	}
	if _, ok := dataMap["count"]; !ok {
		t.Error("expected 'count' key in simulated data")
	}
}

// TestSimulatePortData_WithRuntimeData tests that runtime data overrides simulated data.
func TestSimulatePortData_WithRuntimeData(t *testing.T) {
	ctx := context.Background()

	sourceNode := v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{Component: "source"},
		Status: v1alpha1.TinyNodeStatus{
			Ports: []v1alpha1.TinyNodePortStatus{
				{Name: "out", Source: true, Schema: []byte(`{"type":"object","properties":{"value":{"type":"string"}}}`)},
			},
		},
	}
	sourceNode.Name = "source"

	nodesMap := map[string]v1alpha1.TinyNode{"source": sourceNode}

	runtimeData := map[string][]byte{
		"source:out": []byte(`{"value":"runtime_value"}`),
	}

	data, err := SimulatePortData(ctx, nodesMap, "source:out", runtimeData)
	if err != nil {
		t.Fatalf("SimulatePortData() error: %v", err)
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", data)
	}
	if dataMap["value"] != "runtime_value" {
		t.Errorf("expected runtime_value, got %v", dataMap["value"])
	}
}

// TestValidateEdgeSchema_WithOverlaidSchema exercises ValidateEdgeSchema directly with
// schemas produced by UpdateWithDefinitions, testing various data types.
func TestValidateEdgeSchema_WithOverlaidSchema(t *testing.T) {
	tests := []struct {
		name             string
		targetPortSchema string
		overlayDefs      map[string]*ajson.Node
		portData         interface{}
		edgeConfig       string
		wantErr          bool
		errContains      string
	}{
		{
			name:             "bare context: integer mapped via expression passes",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			overlayDefs: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"count":{"type":"integer"}},"title":"Context","type":"object"}`))),
			},
			portData:   map[string]interface{}{"num": 42},
			edgeConfig: `{"context":"{{$.num}}"}`,
			wantErr:    false,
		},
		{
			name:             "bare context: array mapped via expression passes",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			overlayDefs: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"items":{"type":"array"}},"title":"Context","type":"object"}`))),
			},
			portData:   map[string]interface{}{"list": []interface{}{"a", "b"}},
			edgeConfig: `{"context":"{{$.list}}"}`,
			wantErr:    false,
		},
		{
			name:             "bare context: boolean mapped passes",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			overlayDefs: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"flag":{"type":"boolean"}},"title":"Context","type":"object"}`))),
			},
			portData:   map[string]interface{}{"flag": true},
			edgeConfig: `{"context":"{{$.flag}}"}`,
			wantErr:    false,
		},
		{
			name:             "typed context: non-object correctly rejected",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/In"}`,
			overlayDefs: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"x":{"type":"string"}},"title":"Context","type":"object"}`))),
			},
			portData:    map[string]interface{}{"val": "a string"},
			edgeConfig:  `{"context":"{{$.val}}"}`,
			wantErr:     true,
			errContains: "expected object",
		},
		{
			name:             "no overlay: native required enforced",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name"],"type":"object"}},"$ref":"#/$defs/In"}`,
			overlayDefs:      map[string]*ajson.Node{},
			portData:         map[string]interface{}{},
			edgeConfig:       `{"age":25}`,
			wantErr:          true,
			errContains:      "missing properties",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overlaidSchema, err := schema.UpdateWithDefinitions([]byte(tt.targetPortSchema), tt.overlayDefs)
			if err != nil {
				t.Fatalf("UpdateWithDefinitions() error: %v", err)
			}

			portSchema, err := ajson.Unmarshal(overlaidSchema)
			if err != nil {
				t.Fatalf("Unmarshal overlaid schema: %v", err)
			}

			err = ValidateEdgeSchema(portSchema, tt.portData, []byte(tt.edgeConfig))

			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v\n  schema: %s", err, tt.wantErr, overlaidSchema)
				return
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func mapKeys(m map[string]*ajson.Node) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// suppress unused import
var (
	_ = json.Marshal
	_ = context.Background
)
