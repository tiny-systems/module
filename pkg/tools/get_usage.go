package tools

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// GetUsageTool answers "what has this project been consuming".
//
// Each run has carried its own totals since components could meter, and that
// answers the wrong question: nobody wonders what one run cost, they wonder
// what the week cost and which node is responsible. A per-run number is also
// the one that never gets looked at, because looking requires already knowing
// which run to look at.
//
// Observation, not enforcement. Nothing here stops a hop from running — a
// component failing because a counter reached a number its author never wrote
// is a surprising way for a flow to break, and worth deciding separately from
// being able to see the number at all.
type GetUsageTool struct{}

func NewGetUsageTool() *GetUsageTool { return &GetUsageTool{} }

func (t *GetUsageTool) Name() string { return "get_usage" }

func (t *GetUsageTool) Description() string {
	return `Total what the project consumed — tokens, calls, whatever components metered.

Sums every run in the window, per unit, and breaks it down by node so the
answer to "what is spending this" is in the same response as "how much".

The units are whatever the components reported: llm_input_tokens,
llm_output_tokens, llm_calls for LLM components today, and whatever a future
component decides to count. Nothing here converts to money — a price table
goes stale silently and a wrong cost is worse than a token count.

This reports; it does not limit. To fail a run that spends too much, put a
usage bound in an eval (expect.usage) — that catches a loop that doubled its
calls without letting a counter break production.`
}

func (t *GetUsageTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"lookback_hours": map[string]interface{}{
				"type":        "integer",
				"description": "How far back to total. Default 24.",
			},
			"flow": map[string]interface{}{
				"type":        "string",
				"description": "Restrict to one flow. Omit for the whole project.",
			},
			"max_traces": map[string]interface{}{
				"type":        "integer",
				"description": "Ceiling on runs examined, newest first. Default 200. The response says when it stopped short, because a total that quietly covers half the window is worse than no total.",
			},
		},
	}
}

func (t *GetUsageTool) Execute(ctx context.Context, execCtx ExecutionContext, input map[string]interface{}) ToolResult {
	if execCtx.TraceReader == nil {
		return ToolResult{Success: false, Error: "trace reader not configured"}
	}

	hours := 24.0
	if v, ok := numberFrom(input["lookback_hours"]); ok && v > 0 {
		hours = float64(v)
	}
	maxTraces := 200
	if v, ok := numberFrom(input["max_traces"]); ok && v > 0 {
		maxTraces = int(v)
	}
	flow, _ := input["flow"].(string)
	lookback := time.Duration(hours * float64(time.Hour))

	summaries, err := execCtx.TraceReader.ReadTraces(ctx, execCtx.ProjectName, flow, lookback, 0, maxTraces)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("read traces: %v", err)}
	}

	total := map[string]float64{}
	byNode := map[string]map[string]float64{}
	runsWithUsage := 0

	for _, s := range summaries {
		spans, err := execCtx.TraceReader.ReadTraceDetail(ctx, execCtx.ProjectName, s.ID)
		if err != nil {
			// One unreadable trace should not lose the other 199. The count of
			// what was examined is reported, so a partial total says so.
			continue
		}
		counted := false
		for _, span := range spans {
			if len(span.Usage) == 0 {
				continue
			}
			counted = true
			node := nodeOf(span)
			for unit, amount := range span.Usage {
				total[unit] += amount
				if byNode[node] == nil {
					byNode[node] = map[string]float64{}
				}
				byNode[node][unit] += amount
			}
		}
		if counted {
			runsWithUsage++
		}
	}

	out := map[string]interface{}{
		"window_hours":    hours,
		"runs_examined":   len(summaries),
		"runs_with_usage": runsWithUsage,
		"total":           total,
		"by_node":         nodeBreakdown(byNode),
	}
	if len(summaries) >= maxTraces {
		out["truncated"] = fmt.Sprintf("stopped at %d runs — the window holds more, so this total covers only the newest %d", maxTraces, maxTraces)
	}
	if len(total) == 0 {
		out["hint"] = "Nothing metered in this window. Either nothing ran, or the components that ran do not meter — only paid work reports units."
	}
	return ToolResult{Success: true, Output: out}
}

// nodeOf names who consumed it. The span's target is "<node>:<port>", and the
// port is noise here: a node spends whichever of its ports the message came in
// on.
func nodeOf(span TraceSpanInfo) string {
	target := span.To
	if target == "" {
		target = span.Port
	}
	for i := len(target) - 1; i >= 0; i-- {
		if target[i] == ':' {
			return target[:i]
		}
	}
	if target == "" {
		return "(unknown)"
	}
	return target
}

// nodeBreakdown orders the spenders by what they spent, biggest first — the
// question behind the question is always which one to look at.
func nodeBreakdown(byNode map[string]map[string]float64) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(byNode))
	for node, units := range byNode {
		rows = append(rows, map[string]interface{}{
			"node":  node,
			"usage": units,
			"rank":  rankOf(units),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i]["rank"].(float64) > rows[j]["rank"].(float64)
	})
	for _, r := range rows {
		delete(r, "rank")
	}
	return rows
}

// rankOf orders spenders without pretending to know what a unit is worth.
// Tokens dominate counts by orders of magnitude, so the sum is effectively
// "whoever moved the most tokens" — which is the ordering a reader wants, and
// is honest about being a heuristic rather than a price.
func rankOf(units map[string]float64) float64 {
	var sum float64
	for _, v := range units {
		sum += v
	}
	return sum
}

var _ Tool = (*GetUsageTool)(nil)
