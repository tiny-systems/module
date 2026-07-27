package tools

import (
	"context"
	"strings"
	"time"
)

// Signal readiness gate — the SDK port of the platform playground's
// waitFlowReady (services/grpc-api/playground/readiness.go), shared so every
// send_signal surface can use it instead of re-implementing (or, worse,
// skipping) it.
//
// A freshly-built flow does not run the instant build_flow returns: the
// operator has to reconcile each new TinyNode and its module pod has to route
// the node's messages before a signal fired at the entry can propagate down
// the chain. Fire too early and the signal reaches a live trigger but the
// downstream nodes aren't listening yet — the message goes nowhere and the
// trace shows ONLY the trigger span (spans:1, errors:0), indistinguishable
// from a clean single-node run. That false "clean" is what sends a build
// agent chasing traces that don't exist for a flow that was actually fine —
// it just wasn't awake yet.
//
// The gate is best-effort by design: it waits for node statuses to settle,
// then the caller fires regardless. Readiness is an optimization for the
// fire, never a hard gate that could wedge a loop on a blind spot.

// IsStartFire reports whether a signal payload is an entry-trigger start
// fire — {send:true} (signal fire-once) or {start:true} (ticker/cron/server
// start). Those are the sends that kick a flow into running and the only ones
// worth a readiness wait; a {stop:true} or a status poke is not.
func IsStartFire(data map[string]interface{}) bool {
	send, _ := data["send"].(bool)
	start, _ := data["start"].(bool)
	return send || start
}

// goodNodeStatus is the small set of settled, HEALTHY status tokens the
// operator publishes. Anything non-empty outside this set is either still
// transient (keep waiting) or a hard error (NodeStatusFaults reports it).
var goodNodeStatus = map[string]bool{
	"OK": true, "Running": true, "Ready": true,
	"Started": true, "Stopped": true, "Idle": true,
}

// WaitFlowReady polls the project until the target node's flow stops changing
// status, so a start fire lands on an awake flow instead of a half-reconciled
// one. Scope is the flow that owns nodeID (every element carries its `flow`);
// when the node isn't found the poll covers the whole project rather than
// skipping — a wrong id still deserves the settle wait for whatever it hits.
// Returns the last-seen elements of the scoped flow (for a fault scan without
// another round-trip) and how long it actually waited. Best-effort: on read
// errors or timeout it returns whatever it last saw.
func WaitFlowReady(ctx context.Context, execCtx ExecutionContext, nodeID string) (els []map[string]interface{}, waited time.Duration) {
	if execCtx.ProjectReader == nil || execCtx.ProjectName == "" {
		return nil, 0
	}
	begin := time.Now()
	var last []map[string]interface{}
	prevSig, stable := "", 0
	for i := 0; i < 30; i++ { // ~30 * 700ms ≈ 21s ceiling
		project, err := execCtx.ProjectReader.ReadProjectElements(ctx, execCtx.ProjectName)
		if err == nil && project != nil {
			last = flowScopedElements(project.Elements, nodeID)
			sig, allSettled := statusSignature(last)
			// Fast path: every node already reports a settled status
			// (healthy OR a terminal error) — no reason to keep polling.
			if allSettled {
				return last, time.Since(begin)
			}
			if sig == prevSig {
				stable++
			} else {
				stable, prevSig = 0, sig
			}
			// Some node is still transient, but the picture has been quiet
			// for a couple of ticks — good enough; stop waiting.
			if stable >= 2 && i >= 3 {
				return last, time.Since(begin)
			}
		}
		select {
		case <-ctx.Done():
			return last, time.Since(begin)
		case <-time.After(700 * time.Millisecond):
		}
	}
	return last, time.Since(begin)
}

// flowScopedElements narrows project elements to the flow owning nodeID.
// Falls back to all elements when the node (or its flow tag) isn't found.
func flowScopedElements(all []map[string]interface{}, nodeID string) []map[string]interface{} {
	var flow string
	for _, el := range all {
		if id, _ := el["id"].(string); id == nodeID {
			flow, _ = el["flow"].(string)
			break
		}
	}
	if flow == "" {
		return all
	}
	scoped := make([]map[string]interface{}, 0, len(all))
	for _, el := range all {
		if f, _ := el["flow"].(string); f == flow {
			scoped = append(scoped, el)
		}
	}
	return scoped
}

// statusSignature returns a stable, order-independent fingerprint of every
// node's status plus whether ALL nodes are settled (non-empty status). The
// signature lets WaitFlowReady detect "nothing changed since last poll".
func statusSignature(els []map[string]interface{}) (sig string, allSettled bool) {
	statuses := make([]string, 0, len(els))
	allSettled = true
	for _, el := range els {
		if t, _ := el["type"].(string); t == "tinyEdge" {
			continue
		}
		data, ok := el["data"].(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := el["id"].(string)
		st, _ := data["status"].(string)
		statuses = append(statuses, id+"="+st)
		if st == "" {
			allSettled = false
		}
	}
	// element order from ReadProjectElements is stable across polls, so a
	// plain join is a fine fingerprint without sorting.
	return strings.Join(statuses, "|"), allSettled
}

// NodeStatusFaults reports nodes whose operator status is a hard error — a
// reconcile-time failure that produces NO execution trace and so is invisible
// to a fire-the-entry probe: an RBAC denial, a bad settings value the module
// rejected, a secret that won't resolve. Each fault carries the node's real
// status text so the caller hands the agent the exact reason to fix.
//
// Empty/transient statuses are NOT faults — those are the readiness wait's
// job. Only a non-empty status outside the healthy set counts.
func NodeStatusFaults(els []map[string]interface{}) []string {
	var faults []string
	for _, el := range els {
		if t, _ := el["type"].(string); t == "tinyEdge" {
			continue
		}
		data, ok := el["data"].(map[string]interface{})
		if !ok {
			continue
		}
		st, _ := data["status"].(string)
		if st == "" || goodNodeStatus[st] {
			continue
		}
		id, _ := el["id"].(string)
		faults = append(faults, "node "+id+" failed to come up: "+collapseStatus(st))
	}
	return faults
}

// NotReadyNodes lists node ids still without a settled status after the wait —
// the ones a just-fired signal may not reach. The caller surfaces them so a
// lone-trigger-span trace is explainable instead of a silent mystery.
func NotReadyNodes(els []map[string]interface{}) []string {
	var pending []string
	for _, el := range els {
		if t, _ := el["type"].(string); t == "tinyEdge" {
			continue
		}
		data, ok := el["data"].(map[string]interface{})
		if !ok {
			continue
		}
		if st, _ := data["status"].(string); st == "" {
			id, _ := el["id"].(string)
			pending = append(pending, id)
		}
	}
	return pending
}

// collapseStatus trims an operator status string to one readable line —
// reconcile errors can be long and multi-line.
func collapseStatus(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 240 {
		s = s[:240] + "…"
	}
	return s
}
