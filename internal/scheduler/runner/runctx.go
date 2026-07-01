package runner

import (
	"context"

	m "github.com/tiny-systems/module/module"
)

// Run-context primitives live in the public module package (module/run.go) so
// components can reach BeginRun/RunID — the runner keeps these aliases so its
// call sites and tests read naturally.

// RunInfo is the runner-facing name for a durable run identity.
type RunInfo = m.Run

// NewRunInfo wraps an existing run identity arriving on a message.
func NewRunInfo(runID, stepKey string) *RunInfo { return m.NewRun(runID, stepKey) }

// MintRun starts a new durable run at an entry node.
func MintRun() *RunInfo { return m.MintRun() }

// WithRun attaches a run identity to the context.
func WithRun(ctx context.Context, r *RunInfo) context.Context { return m.WithRun(ctx, r) }

// RunFrom returns the run identity attached by WithRun, if any.
func RunFrom(ctx context.Context) (*RunInfo, bool) { return m.RunFrom(ctx) }
