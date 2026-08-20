package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tiny-systems/module/pkg/evals"
)

// RunEvalTool lets whoever built a flow find out whether it works.
//
// Until now an agent could build a flow and had no way to check it: fire a
// signal, read a trace, squint at payloads, decide. Every one of those steps
// was a separate call and the deciding was done by eye, which means it was
// done inconsistently or not at all — so flows shipped unverified and broke
// quietly, which is how every defect in this system has been found.
//
// Two uses in one tool. With expectations, it is a check that passes or fails.
// Without them, it runs the flow and reports what arrived where — which is the
// material for writing the expectations, so an eval never starts from a blank
// page.
type RunEvalTool struct{}

func NewRunEvalTool() *RunEvalTool { return &RunEvalTool{} }

func (t *RunEvalTool) Name() string { return "run_eval" }

func (t *RunEvalTool) Description() string {
	return `Fire a flow's trigger, wait for the run, and check what came out.

Use it the moment you finish building a flow: a flow you have not run is a
flow you have guessed at.

Call it with a trigger and no expectations first — it runs the flow and
reports every port that received something, with the payloads. That is what
you write the expectations from.

Then call it again with expectations to turn the run into a check:

  expect.arrives: [{at: "<node>:<port>", path: "$.count", equals: 2}]
  expect.errors:  0                      (the default)
  expect.usage:   {llm_calls: {max: 2}}  (catches a loop that doubles its calls)

Conditions: equals, contains, matches (regexp), exists, min, max. An arrival
with no condition asserts only that a message reached that port, which is the
most common breakage. Use contains rather than equals on anything an LLM
wrote — the wording changes and the claim does not.

Port references match by suffix, so "debug-21578:in" keeps working after the
flow id changes.`
}

func (t *RunEvalTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "The claim being checked, e.g. \"pod-watch diagnoses a crashlooping pod\". A failure reports this, so write it as a statement.",
			},
			"flow": map[string]interface{}{
				"type":        "string",
				"description": "Flow to scope the run to. Recommended when the project has other flows running.",
			},
			"trigger": map[string]interface{}{
				"type":        "object",
				"description": "What starts the run.",
				"properties": map[string]interface{}{
					"node": map[string]interface{}{"type": "string", "description": "Full node id to fire at (from read_project)."},
					"port": map[string]interface{}{"type": "string", "description": "Port to deliver on. Default _control — a trigger's Send button."},
					"data": map[string]interface{}{"type": "object", "description": "Payload, e.g. {\"send\": true, \"context\": {...}}."},
				},
				"required": []string{"node"},
			},
			"save": map[string]interface{}{
				"type":        "boolean",
				"description": "Write this eval down so it guards the flow after this session. Do it once a check passes — a check that only ran once inspected the flow, it does not protect it.",
			},
			"timeout_seconds": map[string]interface{}{
				"type":        "integer",
				"description": "How long to wait for the run to settle. Default 60.",
			},
			"expect": map[string]interface{}{
				"type":        "object",
				"description": "What must hold. Omit entirely on the first call to see what the run produces.",
				"properties": map[string]interface{}{
					"errors": map[string]interface{}{"type": "integer", "description": "Errors the run may produce. Default 0."},
					"arrives": map[string]interface{}{
						"type":        "array",
						"description": "Assertions on payloads that reached a port.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"at":       map[string]interface{}{"type": "string", "description": "\"<node>:<port>\", suffix match allowed."},
								"path":     map[string]interface{}{"type": "string", "description": "JSONPath into the payload, e.g. $.rows[0].name. Omit for the whole payload."},
								"equals":   map[string]interface{}{"description": "Exact value."},
								"contains": map[string]interface{}{"type": "string", "description": "Substring — use this for anything an LLM wrote."},
								"matches":  map[string]interface{}{"type": "string", "description": "Regexp."},
								"exists":   map[string]interface{}{"type": "boolean"},
								"min":      map[string]interface{}{"type": "number"},
								"max":      map[string]interface{}{"type": "number"},
							},
							"required": []string{"at"},
						},
					},
					"usage": map[string]interface{}{
						"type":        "object",
						"description": "Bounds per metered unit, e.g. {\"llm_calls\": {\"max\": 2}}.",
					},
				},
			},
		},
		"required": []string{"trigger"},
	}
}

func (t *RunEvalTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.EvalRunner == nil {
		return ToolResult{Success: false, Error: "eval runner not configured"}
	}

	spec, err := specFromInput(input)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}

	outcome, err := execCtx.EvalRunner.RunEval(ctx, execCtx.ProjectName, spec)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}

	out := map[string]interface{}{
		"name":     spec.Name,
		"passed":   outcome.Passed,
		"errors":   outcome.Errors,
		"trace_id": outcome.TraceID,
	}
	if len(outcome.Failures) > 0 {
		out["failures"] = outcome.Failures
	}
	if len(outcome.Usage) > 0 {
		out["usage"] = outcome.Usage
	}

	// With no expectations the run itself is the answer: what arrived, and
	// where. This is what the next call's assertions are written from.
	if len(spec.Expect.Arrives) == 0 && len(spec.Expect.Usage) == 0 {
		out["arrived"] = arrivalReport(outcome.Observed)
		out["hint"] = "No expectations were given, so this only ran the flow. Turn the payloads above into expect.arrives to make it a check — assert what must be true, not everything that happened."
	}

	if save, _ := input["save"].(bool); save {
		out = t.save(ctx, execCtx, spec, outcome, out)
	}

	return ToolResult{Success: true, Output: out}
}

// save writes the eval down, and says plainly when it has just written a check
// that does not currently hold — a red check saved on purpose is a decision,
// one saved by accident is a lie the next reader inherits.
func (t *RunEvalTool) save(ctx context.Context, execCtx ExecutionContext, spec evals.Spec, outcome EvalOutcome, out map[string]interface{}) map[string]interface{} {
	if execCtx.EvalStore == nil {
		out["save_error"] = "this host cannot save evals"
		return out
	}
	if len(spec.Expect.Arrives) == 0 && len(spec.Expect.Usage) == 0 {
		out["save_error"] = "nothing to save: an eval with no expectations asserts nothing. Add expect.arrives first."
		return out
	}
	location, err := execCtx.EvalStore.SaveEval(ctx, execCtx.ProjectName, spec)
	if err != nil {
		out["save_error"] = err.Error()
		return out
	}
	out["saved_to"] = location
	if !outcome.Passed {
		out["save_warning"] = "saved a check that does not currently pass — it will fail until the flow is fixed"
	}
	return out
}

// arrivalReport summarises what reached each port, with the first payload as
// the sample to write assertions against. Truncated, because a trace can carry
// a lot and the shape is what matters here.
func arrivalReport(got evals.Observed) []map[string]interface{} {
	report := make([]map[string]interface{}, 0, len(got.Payloads))
	for port, payloads := range got.Payloads {
		entry := map[string]interface{}{"at": port, "messages": len(payloads)}
		if len(payloads) > 0 {
			entry["sample"] = truncatePayload(payloads[0], 600)
		}
		report = append(report, entry)
	}
	return report
}

func truncatePayload(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "… (truncated)"
}

// specFromInput builds the spec through JSON so the tool and the file format
// stay one shape — a divergence between what an agent writes and what a repo
// holds would show up as an eval that runs in one place and not the other.
func specFromInput(input map[string]interface{}) (evals.Spec, error) {
	raw := map[string]interface{}{}
	for _, key := range []string{"name", "flow", "trigger", "expect"} {
		if v, ok := input[key]; ok {
			raw[key] = v
		}
	}
	if _, ok := raw["name"]; !ok {
		raw["name"] = "unnamed eval"
	}
	if seconds, ok := numberFrom(input["timeout_seconds"]); ok && seconds > 0 {
		raw["timeout"] = fmt.Sprintf("%ds", int(seconds))
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return evals.Spec{}, err
	}
	specs, err := evals.Parse("run_eval", encoded)
	if err != nil {
		return evals.Spec{}, err
	}
	if len(specs) != 1 {
		return evals.Spec{}, fmt.Errorf("expected one eval, got %d", len(specs))
	}
	return specs[0], nil
}

var _ Tool = (*RunEvalTool)(nil)
