package tools

import (
	"context"
	"fmt"
)

// ReplayRunTool re-drives a run that already happened and reports what changed.
//
// The tool to reach for after editing a flow, and the one to reach for after a
// module release. run_eval checks the claims somebody thought to write down;
// this checks real traffic, which is a far larger set and needs nobody to have
// predicted anything.
//
// It compares structure, not values. A model rewording its answer is not a
// change; a field that stopped arriving, or arrived as a string where it was a
// number, is — and that is what a release breaks.
type ReplayRunTool struct{}

func NewReplayRunTool() *ReplayRunTool { return &ReplayRunTool{} }

func (t *ReplayRunTool) Name() string { return "replay_run" }

func (t *ReplayRunTool) Description() string {
	return `Re-drive a recorded run and report what changed since.

Use it after editing a flow, and after installing a module version: take a
trace from get_traces and ask whether the same traffic still behaves.

By default it re-drives the run's entry, so the whole flow runs again. Pass
"at" to re-drive one hop instead, naming the port the message was delivered to.

What comes back is a structural diff: ports that stopped receiving, ports that
started, and payloads whose shape changed. Values are expected to differ — a
model rewords, a cluster moves on — so they are not compared.

Two things worth knowing before calling it. A replay re-runs side effects: a
run that restarted a workload restarts it again. And credentials are redacted
in a recording, so a hop carrying one cannot be replayed as data — where that
hop comes off a trigger the trigger is fired instead, which rebuilds its own
context; anywhere else the call is refused and points at the entry.`
}

func (t *ReplayRunTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"trace_id": map[string]interface{}{
				"type":        "string",
				"description": "The run to re-drive, from get_traces.",
			},
			"at": map[string]interface{}{
				"type":        "string",
				"description": "Re-drive only the hop delivered to this port, e.g. \"debug-21578:in\" (suffix match). Omit to re-drive the run's entry, which runs the whole flow.",
			},
		},
		"required": []string{"trace_id"},
	}
}

func (t *ReplayRunTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.ReplayRunner == nil {
		return ToolResult{Success: false, Error: "replay runner not configured"}
	}
	traceID, _ := input["trace_id"].(string)
	if traceID == "" {
		return ToolResult{Success: false, Error: "trace_id is required — take one from get_traces"}
	}
	at, _ := input["at"].(string)

	outcome, err := execCtx.ReplayRunner.ReplayRun(ctx, execCtx.ProjectName, traceID, at)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}

	out := map[string]interface{}{
		"hop":       outcome.Hop,
		"unchanged": outcome.Unchanged,
		"compared":  outcome.Compared,
	}
	if len(outcome.Changes) > 0 {
		out["changes"] = outcome.Changes
	}
	switch {
	case outcome.Compared == 0:
		out["hint"] = "Nothing was compared, so this proves nothing — the recorded run may have had no hops below the replayed one."
	case outcome.Unchanged:
		out["hint"] = fmt.Sprintf("%d port(s) reached with the same shape as the recording.", outcome.Compared)
	default:
		out["hint"] = "Each change is a port whose behaviour differs from the recording. A shape change is the kind a module release causes; a port that stopped receiving means the run took a different path."
	}
	return ToolResult{Success: true, Output: out}
}

var _ Tool = (*ReplayRunTool)(nil)
