package tools

import (
	"context"
	"reflect"
	"testing"
)

// captureEdgeConfigurer records the schema argument passed to ConfigureEdge
// so tests can assert what flowed through after the edit_flow tool's
// inference step.
type captureEdgeConfigurer struct {
	gotConfig map[string]interface{}
	gotSchema map[string]interface{}
}

func (c *captureEdgeConfigurer) ConfigureEdge(_ context.Context, _, _, _ string, config, schema map[string]interface{}, _ string) (*ConfigureEdgeResult, error) {
	c.gotConfig = config
	c.gotSchema = schema
	return &ConfigureEdgeResult{Valid: true}, nil
}

// captureNodeSettingsConfigurer records the schema arg into ConfigureNodeSettings.
type captureNodeSettingsConfigurer struct {
	gotSettings map[string]interface{}
	gotSchema   map[string]interface{}
}

func (c *captureNodeSettingsConfigurer) ConfigureNodeSettings(_ context.Context, _, _, _ string, settings, schema map[string]interface{}) (*ConfigureNodeSettingsResult, error) {
	c.gotSettings = settings
	c.gotSchema = schema
	return &ConfigureNodeSettingsResult{Valid: true}, nil
}

// TestEditFlow_ConfigureEdge_NoInferenceWhenSchemaMissing pins the
// post-v0.10.7 contract: the SDK no longer fabricates a schema from the
// configuration data. If the caller omits 'schema', it stays nil and
// downstream validation enforces correctness. Inference was removed
// because it taught models to skip schema declarations, which then
// produced silent gaps in edge validation.
func TestEditFlow_ConfigureEdge_NoInferenceWhenSchemaMissing(t *testing.T) {
	cap := &captureEdgeConfigurer{}
	execCtx := ExecutionContext{
		EdgeConfigurer: cap,
		ProjectName:    "p1",
		FlowName:       "f1",
	}
	tool := NewEditFlowTool()

	res := tool.Execute(context.Background(), execCtx, map[string]interface{}{
		"action":  "configure_edge",
		"edge_id": "edge-1",
		"configuration": map[string]interface{}{
			"token":   "secret",
			"port":    8080,
			"enabled": true,
		},
		// note: no "schema" key
	})

	if !res.Success {
		t.Fatalf("expected success, got error: %v", res.Error)
	}
	if cap.gotSchema != nil {
		t.Errorf("expected schema to remain nil (no inference), got %#v", cap.gotSchema)
	}
}

// TestEditFlow_ConfigureEdge_RespectsExplicitSchema proves an explicit
// caller-supplied schema is NOT overwritten by the inferrer.
func TestEditFlow_ConfigureEdge_RespectsExplicitSchema(t *testing.T) {
	cap := &captureEdgeConfigurer{}
	execCtx := ExecutionContext{
		EdgeConfigurer: cap,
		ProjectName:    "p1",
		FlowName:       "f1",
	}
	tool := NewEditFlowTool()

	explicit := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"token": map[string]interface{}{"type": "string", "secret": true, "title": "API Token"},
		},
	}
	res := tool.Execute(context.Background(), execCtx, map[string]interface{}{
		"action":        "configure_edge",
		"edge_id":       "edge-1",
		"configuration": map[string]interface{}{"token": "abc"},
		"schema":        explicit,
	})

	if !res.Success {
		t.Fatalf("expected success, got error: %v", res.Error)
	}
	if !reflect.DeepEqual(cap.gotSchema, explicit) {
		t.Errorf("explicit schema should have been preserved verbatim\n got: %#v\nwant: %#v", cap.gotSchema, explicit)
	}
}

// TestEditFlow_ConfigureNode_NoInferenceWhenSchemaMissing is the same
// guarantee for the configure_node path: omitting schema leaves it nil
// rather than fabricating one from the data shape.
func TestEditFlow_ConfigureNode_NoInferenceWhenSchemaMissing(t *testing.T) {
	cap := &captureNodeSettingsConfigurer{}
	execCtx := ExecutionContext{
		NodeSettingsConfigurer: cap,
		ProjectName:            "p1",
		FlowName:               "f1",
	}
	tool := NewEditFlowTool()

	res := tool.Execute(context.Background(), execCtx, map[string]interface{}{
		"action":  "configure_node",
		"node_id": "node-1",
		"settings": map[string]interface{}{
			"delay": 1000,
			"context": map[string]interface{}{
				"token": "secret",
			},
		},
	})

	if !res.Success {
		t.Fatalf("expected success, got error: %v", res.Error)
	}
	if cap.gotSchema != nil {
		t.Errorf("expected schema to remain nil (no inference), got %#v", cap.gotSchema)
	}
}

// TestEditFlow_ConfigureEdge_EmptyConfigDoesNotInfer covers the
// edge-case where the caller intentionally passes an empty configuration
// — we shouldn't fabricate a schema for the absence of data.
func TestEditFlow_ConfigureEdge_EmptyConfigDoesNotInfer(t *testing.T) {
	cap := &captureEdgeConfigurer{}
	execCtx := ExecutionContext{
		EdgeConfigurer: cap,
		ProjectName:    "p1",
		FlowName:       "f1",
	}
	tool := NewEditFlowTool()

	res := tool.Execute(context.Background(), execCtx, map[string]interface{}{
		"action":        "configure_edge",
		"edge_id":       "edge-1",
		"configuration": map[string]interface{}{},
	})

	if !res.Success {
		t.Fatalf("expected success, got error: %v", res.Error)
	}
	if cap.gotSchema != nil {
		t.Errorf("expected schema to remain nil for empty configuration, got %#v", cap.gotSchema)
	}
}
