package runner

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/google/uuid"
)

// RunInfo is the identity of a durable run as it threads through one handler
// invocation. It rides the context from MsgHandler (which attaches it from the
// incoming message, or mints it at the entry node of a durable flow) down to
// sendToEdgeWithRetry (which stamps it onto outgoing edge messages).
//
// StepKey is the idempotency key of the message being handled; NextStepKey
// derives the key for an outgoing edge message. Derivation must be
// DETERMINISTIC across a redelivery: the broker dedupes on Nats-Msg-Id, so a
// re-executed handler must re-emit with byte-identical keys. That is why the
// sequence counters are per-edge, not global — concurrent fan-out to different
// edges (outputHandler runs one goroutine per edge) must not let scheduling
// order change any edge's key.
type RunInfo struct {
	RunID   string
	StepKey string

	seqMu sync.Mutex
	seq   map[string]int
	// emits collects the durable hops published while handling this message,
	// so the step's ledger record can carry them for re-drive.
	emits []EmitRecord
}

// NewRunInfo wraps an existing run identity arriving on a message.
func NewRunInfo(runID, stepKey string) *RunInfo {
	return &RunInfo{RunID: runID, StepKey: stepKey, seq: map[string]int{}}
}

// MintRun starts a new durable run at an entry node. The seed StepKey is
// distinct per run so first-hop keys can't collide across runs.
func MintRun() *RunInfo {
	id := uuid.NewString()
	return &RunInfo{RunID: id, StepKey: id + ".root", seq: map[string]int{}}
}

// NextStepKey derives the idempotency key for the next emit over edgeID.
// Fixed length regardless of chain depth (a hash, not an append), and
// deterministic per (parent StepKey, edgeID, per-edge emit ordinal) — repeated
// emits to the same edge within one handler invocation get 0,1,2,…, which is
// stable as long as the component's emit order is deterministic.
func (r *RunInfo) NextStepKey(edgeID string) string {
	r.seqMu.Lock()
	n := r.seq[edgeID]
	r.seq[edgeID] = n + 1
	r.seqMu.Unlock()

	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%s|%s|%d", r.StepKey, edgeID, n)
	return fmt.Sprintf("%s.%016x", r.RunID, h.Sum64())
}

// RecordEmit notes a durable hop published while handling this message.
func (r *RunInfo) RecordEmit(e EmitRecord) {
	r.seqMu.Lock()
	r.emits = append(r.emits, e)
	r.seqMu.Unlock()
}

// Emits returns the durable hops recorded so far.
func (r *RunInfo) Emits() []EmitRecord {
	r.seqMu.Lock()
	defer r.seqMu.Unlock()
	return append([]EmitRecord(nil), r.emits...)
}

type runCtxKey struct{}

// WithRun attaches a run identity to the context for the duration of one
// handler invocation.
func WithRun(ctx context.Context, r *RunInfo) context.Context {
	return context.WithValue(ctx, runCtxKey{}, r)
}

// RunFrom returns the run identity attached by WithRun, if any.
func RunFrom(ctx context.Context) (*RunInfo, bool) {
	r, ok := ctx.Value(runCtxKey{}).(*RunInfo)
	return r, ok
}
