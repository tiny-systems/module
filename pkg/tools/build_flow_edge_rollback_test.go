package tools

import (
	"context"
	"strings"
	"testing"
)

// rejectingEdgeConfigurer refuses every configuration, standing in for a
// mapping that fails the target port's schema.
type rejectingEdgeConfigurer struct{}

func (rejectingEdgeConfigurer) ConfigureEdge(_ context.Context, _, _, _ string, _, _ map[string]interface{}, _ string) (*ConfigureEdgeResult, error) {
	return &ConfigureEdgeResult{Valid: false, Error: "expected string, but got object"}, nil
}

// recordingFlowModifier captures the operations build_flow applies so a test
// can assert the rejected edge was actually unwired.
type recordingFlowModifier struct {
	ops []FlowOperation
}

func (r *recordingFlowModifier) ApplyFlowChanges(_ context.Context, _, _ string, ops []FlowOperation) ([]OperationResult, error) {
	r.ops = append(r.ops, ops...)
	out := make([]OperationResult, len(ops))
	for i, op := range ops {
		out[i] = OperationResult{Op: op.Op, ID: op.ID, Success: true}
	}
	return out, nil
}

func buildOneEdgeFlow(t *testing.T, execCtx ExecutionContext) ToolResult {
	t.Helper()
	return NewBuildFlowTool().Execute(context.Background(), execCtx, map[string]interface{}{
		"nodes": []interface{}{
			map[string]interface{}{"alias": "a", "component": "x", "module": "m"},
			map[string]interface{}{"alias": "b", "component": "y", "module": "m"},
		},
		"edges": []interface{}{
			map[string]interface{}{
				"from":          "a:out",
				"to":            "b:in",
				"configuration": map[string]interface{}{"v": "{{$.nope}}"},
			},
		},
	})
}

func buildErrors(t *testing.T, res ToolResult) []string {
	t.Helper()
	out, ok := res.Output.(map[string]interface{})
	if !ok {
		t.Fatalf("expected a map output, got %T", res.Output)
	}
	errs, _ := out["errors"].([]string)
	return errs
}

// ConfigureEdge refuses to persist a configuration that fails validation, but
// AddEdge has already wired the edge by then. Left in place, that edge carries
// NO mapping — and at runtime the target silently falls back to its own
// settings example, so the flow serves fabricated data behind a green build.
// The edge must be removed so the breakage is visible instead.
func TestBuildFlowUnwiresEdgeWhoseConfigWasRejected(t *testing.T) {
	mod := &recordingFlowModifier{}

	res := buildOneEdgeFlow(t, ExecutionContext{
		ProjectName:    "p",
		FlowName:       "f",
		NodeAdder:      &countingNodeAdder{},
		EdgeAdder:      &countingEdgeAdder{},
		EdgeConfigurer: rejectingEdgeConfigurer{},
		FlowModifier:   mod,
	})

	var removed bool
	for _, op := range mod.ops {
		if op.Op == "delete" && op.ID == "e1" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("rejected edge was left wired without configuration; ops=%+v", mod.ops)
	}

	// build_flow intentionally returns partial results (Success stays true so
	// the caller keeps the nodes it did create), so the rejection has to be
	// unmistakable in errors[] — a warning would let it slide by.
	if errs := buildErrors(t, res); len(errs) == 0 {
		t.Fatalf("rejection must be an error, not a warning; output=%+v", res.Output)
	}

	out, _ := res.Output.(map[string]interface{})
	edges, _ := out["edges_created"].([]map[string]interface{})
	if len(edges) != 1 {
		t.Fatalf("expected one edge entry, got %+v", out["edges_created"])
	}
	if valid, _ := edges[0]["config_valid"].(bool); valid {
		t.Error("edge should be reported config_valid=false")
	}
	if wasRemoved, _ := edges[0]["removed"].(bool); !wasRemoved {
		t.Errorf("edge should be reported as removed; got %+v", edges[0])
	}
}

// Without a FlowModifier the edge cannot be removed. That is still a failure —
// and the message must say the edge is live and unconfigured, so the author
// knows to delete it rather than assuming build_flow cleaned up.
func TestBuildFlowReportsWhenRejectedEdgeCannotBeUnwired(t *testing.T) {
	res := buildOneEdgeFlow(t, ExecutionContext{
		ProjectName:    "p",
		FlowName:       "f",
		NodeAdder:      &countingNodeAdder{},
		EdgeAdder:      &countingEdgeAdder{},
		EdgeConfigurer: rejectingEdgeConfigurer{},
		FlowModifier:   nil,
	})

	errs := buildErrors(t, res)
	if len(errs) == 0 {
		t.Fatalf("expected an error; output=%+v", res.Output)
	}
	var flagged bool
	for _, e := range errs {
		if strings.Contains(e, "WITHOUT configuration") {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("error must flag the edge as live-but-unconfigured; got %v", errs)
	}
}
