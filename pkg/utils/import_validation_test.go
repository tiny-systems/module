package utils

import (
	"strings"
	"testing"
)

func TestValidateProjectImport_BareConfigurableWithStructuredEdgeConfig(t *testing.T) {
	// Edge maps structured object into a configurable field, but target handle's
	// $def is bare (no properties). Validation should catch this.
	data := &ProjectExport{
		TinyFlows: []ExportFlow{{ResourceName: "flow1", Name: "Flow 1"}},
		Elements: []map[string]interface{}{
			// Target node with bare configurable Context
			{
				"id":   "node-target",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "router",
					"module":    "tinysystems/common-module-v0",
					"handles": []interface{}{
						map[string]interface{}{
							"id":   "input",
							"type": "target",
							"schema": map[string]interface{}{
								"$ref": "#/$defs/Request",
								"$defs": map[string]interface{}{
									"Context": map[string]interface{}{
										"configurable": true,
										// BARE — no properties, no type
									},
									"Request": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"context": map[string]interface{}{
												"$ref": "#/$defs/Context",
											},
											"name": map[string]interface{}{
												"type": "string",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Source node
			{
				"id":   "node-source",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "http_server",
					"module":    "tinysystems/http-module-v0",
					"handles":   []interface{}{},
				},
			},
			// Edge that maps structured object into "context"
			{
				"id":           "edge1",
				"type":         TinyEdgeType,
				"flow":         "flow1",
				"source":       "node-source",
				"sourceHandle": "request",
				"target":       "node-target",
				"targetHandle": "input",
				"data": map[string]interface{}{
					"configuration": map[string]interface{}{
						"context": map[string]interface{}{
							"firestore_project_id": "{{$.context.firestore_project_id}}",
							"google_credentials":   "{{$.context.google_credentials}}",
						},
						"name": "{{$.name}}",
					},
				},
			},
		},
	}

	errors, _ := ValidateProjectImport(data)

	// Should have an error about bare configurable Context receiving structured data
	found := false
	for _, e := range errors {
		if strings.Contains(e, "configurable") && strings.Contains(e, "no properties") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error about bare configurable def receiving structured data, got errors: %v", errors)
	}
}

func TestValidateProjectImport_ConfigurableWithSchemaIsOK(t *testing.T) {
	// Same setup but Context has proper properties — should NOT error.
	data := &ProjectExport{
		TinyFlows: []ExportFlow{{ResourceName: "flow1", Name: "Flow 1"}},
		Elements: []map[string]interface{}{
			{
				"id":   "node-target",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "router",
					"module":    "tinysystems/common-module-v0",
					"handles": []interface{}{
						map[string]interface{}{
							"id":   "input",
							"type": "target",
							"schema": map[string]interface{}{
								"$ref": "#/$defs/Request",
								"$defs": map[string]interface{}{
									"Context": map[string]interface{}{
										"configurable": true,
										"type":         "object",
										"properties": map[string]interface{}{
											"firestore_project_id": map[string]interface{}{"type": "string"},
											"google_credentials":   map[string]interface{}{"type": "string"},
										},
									},
									"Request": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"context": map[string]interface{}{
												"$ref": "#/$defs/Context",
											},
											"name": map[string]interface{}{
												"type": "string",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			{
				"id":   "node-source",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "http_server",
					"module":    "tinysystems/http-module-v0",
					"handles":   []interface{}{},
				},
			},
			{
				"id":           "edge1",
				"type":         TinyEdgeType,
				"flow":         "flow1",
				"source":       "node-source",
				"sourceHandle": "request",
				"target":       "node-target",
				"targetHandle": "input",
				"data": map[string]interface{}{
					"configuration": map[string]interface{}{
						"context": map[string]interface{}{
							"firestore_project_id": "{{$.context.firestore_project_id}}",
							"google_credentials":   "{{$.context.google_credentials}}",
						},
						"name": "{{$.name}}",
					},
				},
			},
		},
	}

	errors, _ := ValidateProjectImport(data)

	for _, e := range errors {
		if strings.Contains(e, "configurable") && strings.Contains(e, "no properties") {
			t.Errorf("unexpected bare configurable error: %s", e)
		}
	}
}

func TestValidateProjectImport_PassthroughContextIsOK(t *testing.T) {
	// Edge passes context as string expression "{{$.context}}" — not structured.
	// Should NOT trigger the bare configurable error even if Context is bare.
	data := &ProjectExport{
		TinyFlows: []ExportFlow{{ResourceName: "flow1", Name: "Flow 1"}},
		Elements: []map[string]interface{}{
			{
				"id":   "node-target",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "debug",
					"module":    "tinysystems/common-module-v0",
					"handles": []interface{}{
						map[string]interface{}{
							"id":   "in",
							"type": "target",
							"schema": map[string]interface{}{
								"$ref": "#/$defs/Request",
								"$defs": map[string]interface{}{
									"Context": map[string]interface{}{
										"configurable": true,
									},
									"Request": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"context": map[string]interface{}{
												"$ref": "#/$defs/Context",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			{
				"id":   "node-source",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "ticker",
					"module":    "tinysystems/common-module-v0",
					"handles":   []interface{}{},
				},
			},
			{
				"id":           "edge1",
				"type":         TinyEdgeType,
				"flow":         "flow1",
				"source":       "node-source",
				"sourceHandle": "out",
				"target":       "node-target",
				"targetHandle": "in",
				"data": map[string]interface{}{
					"configuration": map[string]interface{}{
						"context": "{{$.context}}",
					},
				},
			},
		},
	}

	errors, _ := ValidateProjectImport(data)

	for _, e := range errors {
		if strings.Contains(e, "configurable") && strings.Contains(e, "no properties") {
			t.Errorf("unexpected bare configurable error for passthrough: %s", e)
		}
	}
}

func TestValidateProjectImport_NonConfigurableFieldIsIgnored(t *testing.T) {
	// Edge maps object into a field whose $def is NOT configurable — should be ignored.
	data := &ProjectExport{
		TinyFlows: []ExportFlow{{ResourceName: "flow1", Name: "Flow 1"}},
		Elements: []map[string]interface{}{
			{
				"id":   "node-target",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "some_component",
					"module":    "tinysystems/some-module-v0",
					"handles": []interface{}{
						map[string]interface{}{
							"id":   "request",
							"type": "target",
							"schema": map[string]interface{}{
								"$ref": "#/$defs/Request",
								"$defs": map[string]interface{}{
									"Settings": map[string]interface{}{
										// NOT configurable
										"type": "object",
										"properties": map[string]interface{}{
											"host": map[string]interface{}{"type": "string"},
										},
									},
									"Request": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"settings": map[string]interface{}{
												"$ref": "#/$defs/Settings",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			{
				"id":   "node-source",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "other",
					"module":    "tinysystems/some-module-v0",
					"handles":   []interface{}{},
				},
			},
			{
				"id":           "edge1",
				"type":         TinyEdgeType,
				"flow":         "flow1",
				"source":       "node-source",
				"sourceHandle": "out",
				"target":       "node-target",
				"targetHandle": "request",
				"data": map[string]interface{}{
					"configuration": map[string]interface{}{
						"settings": map[string]interface{}{
							"host": "{{$.host}}",
						},
					},
				},
			},
		},
	}

	errors, _ := ValidateProjectImport(data)

	for _, e := range errors {
		if strings.Contains(e, "configurable") && strings.Contains(e, "no properties") {
			t.Errorf("unexpected error for non-configurable field: %s", e)
		}
	}
}
