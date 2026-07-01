package runner

import m "github.com/tiny-systems/module/module"

// Step-ledger types live in the public module package (module/ledger.go) —
// their JSON shape is a stable contract that status components parse. The
// runner keeps these aliases so its call sites and tests read naturally.

// EmitRecord is one durable hop published by a completed step.
type EmitRecord = m.EmitRecord

// StepRecord is the completion record of one durable hop.
type StepRecord = m.StepRecord

// Step statuses.
const (
	StepStatusDone   = m.StepStatusDone
	StepStatusFailed = m.StepStatusFailed
)

// StepLedgerKey returns the execution-scoped State key for a hop's record.
func StepLedgerKey(stepKey string) string { return m.StepLedgerKey(stepKey) }
