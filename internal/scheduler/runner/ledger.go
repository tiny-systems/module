package runner

import "time"

// The step ledger is the durable-execution completion record, written to the
// execution-scoped State (JetStream KV) after a durable hop's handler
// finishes. It serves three jobs:
//
//  1. Redelivery protection beyond the broker's duplicate window: a hop whose
//     ledger record exists is skipped on redelivery — the component (and its
//     side effects: LLM spend, sends, writes) never re-executes.
//  2. Re-drive: each record carries the hops the step emitted (including
//     payload), so the stuck-run reconciler can re-publish a frontier hop
//     whose own record never appeared.
//  3. Observability: a run's progress is readable straight out of the KV.
//
// Records are keyed StepLedgerKey(stepKey) inside Scoped(execution, runID).

// EmitRecord is one durable hop published by a completed step — everything
// needed to re-publish it verbatim.
type EmitRecord struct {
	To      string `json:"to"`
	From    string `json:"from"`
	EdgeID  string `json:"edgeID"`
	StepKey string `json:"stepKey"`
	Data    []byte `json:"data"`
}

// Step statuses. A failed step is terminal for the run reconciler — business
// retries belong to explicit retry components / edge RetryPolicy, not the
// durability layer.
const (
	StepStatusDone   = "done"
	StepStatusFailed = "failed"
)

// StepRecord is the completion record of one durable hop.
type StepRecord struct {
	Node        string       `json:"node"`
	Status      string       `json:"status"`
	Error       string       `json:"error,omitempty"`
	Emits       []EmitRecord `json:"emits,omitempty"`
	CompletedAt time.Time    `json:"completedAt"`
}

// stepLedgerPrefix namespaces ledger records inside the execution scope.
const stepLedgerPrefix = "step/"

// StepLedgerKey returns the execution-scoped State key for a hop's record.
func StepLedgerKey(stepKey string) string { return stepLedgerPrefix + stepKey }
