package utils

import (
	"strings"
	"testing"
)

func TestValidateProjectImport_ForwardingEdgeWithNoConfigIsNotBlocked(t *testing.T) {
	// An error→response / router-default forwarding edge legitimately carries
	// no data.configuration. A running project exports exactly this, so import
	// must NOT block on it — warn at most. This is the export→import round-trip
	// a real project (e.g. My IP Address) depends on.
	data := &ProjectExport{
		TinyFlows: []ExportFlow{{ResourceName: "flow1", Name: "Flow 1"}},
		Elements: []map[string]interface{}{
			{
				"id": "node-a", "type": TinyNodeType, "flow": "flow1",
				"data": map[string]interface{}{
					"component": "http_client", "module": "tinysystems/http-module-v0",
					"handles": []interface{}{},
				},
			},
			{
				"id": "node-b", "type": TinyNodeType, "flow": "flow1",
				"data": map[string]interface{}{
					"component": "http_server", "module": "tinysystems/http-module-v0",
					"handles": []interface{}{},
				},
			},
			{
				"id": "edge1", "type": TinyEdgeType, "flow": "flow1",
				"source": "node-a", "sourceHandle": "error",
				"target": "node-b", "targetHandle": "response",
				"data": map[string]interface{}{}, // no configuration — pure forwarding
			},
		},
	}

	errors, _ := ValidateProjectImport(data)
	for _, e := range errors {
		if strings.Contains(e, "configuration") {
			t.Errorf("forwarding edge with no config must be advisory, not a blocking error: %v", e)
		}
	}
}

func TestValidateProjectImport_BareConfigurableWithStructuredEdgeConfig(t *testing.T) {
	// Edge maps structured object into a configurable field, but target handle's
	// $def is bare (no properties). This degrades faker/simulation fidelity but
	// does NOT break the running flow, so it must surface as a WARNING — never a
	// blocking error, or a real project's export→import round-trip fails.
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

	errors, warnings := ValidateProjectImport(data)

	// Must surface as a WARNING about the bare configurable def...
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "configurable") && strings.Contains(w, "no properties") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about bare configurable def receiving structured data, got warnings: %v", warnings)
	}
	// ...and must NOT be a blocking error (that would break the import round-trip).
	for _, e := range errors {
		if strings.Contains(e, "configurable") && strings.Contains(e, "no properties") {
			t.Errorf("bare configurable def must be advisory, not a blocking error: %v", e)
		}
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

func TestValidateProjectImport_EmptyObjectIsOK(t *testing.T) {
	// Edge maps empty object {} into a bare configurable field.
	// This is normal — means "no custom data for this field". Should NOT error.
	data := &ProjectExport{
		TinyFlows: []ExportFlow{{ResourceName: "flow1", Name: "Flow 1"}},
		Elements: []map[string]interface{}{
			{
				"id":   "node-target",
				"type": TinyNodeType,
				"flow": "flow1",
				"data": map[string]interface{}{
					"component": "go_template",
					"module":    "tinysystems/encoding-module-v0",
					"handles": []interface{}{
						map[string]interface{}{
							"id":   "request",
							"type": "target",
							"schema": map[string]interface{}{
								"$ref": "#/$defs/Request",
								"$defs": map[string]interface{}{
									"Renderdata": map[string]interface{}{
										"configurable": true,
										// Bare — no properties
									},
									"Request": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"renderData": map[string]interface{}{
												"$ref": "#/$defs/Renderdata",
											},
											"template": map[string]interface{}{
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
					"component": "router",
					"module":    "tinysystems/common-module-v0",
					"handles":   []interface{}{},
				},
			},
			{
				"id":           "edge1",
				"type":         TinyEdgeType,
				"flow":         "flow1",
				"source":       "node-source",
				"sourceHandle": "default",
				"target":       "node-target",
				"targetHandle": "request",
				"data": map[string]interface{}{
					"configuration": map[string]interface{}{
						"renderData": map[string]interface{}{},
						"template":   "home.html",
					},
				},
			},
		},
	}

	errors, _ := ValidateProjectImport(data)

	for _, e := range errors {
		if strings.Contains(e, "configurable") && strings.Contains(e, "no properties") {
			t.Errorf("unexpected error for empty object mapping: %s", e)
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
