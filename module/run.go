package module

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"

	"github.com/google/uuid"
)

// Durable-run identity. A Run threads through one handler invocation on the
// context: the runtime attaches it (from the incoming message, or by minting
// at the entry node of a durable flow), and the edge dispatcher stamps
// outgoing hops with it so emits ride the durable (fire-and-forget,
// work-queue) path instead of blocking.
//
// Component authors interact with runs through two functions only:
//
//   - BeginRun — start a durable run from a CLASSIC (blocking) component.
//     This is the front-door bridge: a gateway component (e.g. behind
//     http_server) calls BeginRun, emits the chain's first message with the
//     returned context (that emit is durable and returns immediately), and
//     replies synchronously to its caller with the run id.
//   - RunID — read the current run's id, e.g. to hand to a client for
//     status polling.
//
// Everything else on Run is runtime machinery — exported for the scheduler
// and runner, not for components.

// Run is the identity of a durable run during one handler invocation.
// StepKey is the idempotency key of the message being handled; NextStepKey
// derives keys for outgoing hops. Derivation must be DETERMINISTIC across a
// redelivery: the broker dedupes on Nats-Msg-Id, so a re-executed handler
// must re-emit byte-identical keys. That is why the sequence counters are
// per-edge, not global — concurrent fan-out to different edges must not let
// scheduling order change any edge's key.
type Run struct {
	RunID   string
	StepKey string

	// Reply addressing lets a durable run deliver a synchronous response
	// back to the exact instance that started it (an http_server pod holding
	// an open connection), instead of round-robining to any queue-group
	// member. Set when the run is minted by a synchronous front door; empty
	// for pure background runs.
	//   ReplySubject — the core-NATS subject the origin instance subscribes
	//     to; the hop targeting ReplyTarget is delivered here, not via the
	//     durable work-queue.
	//   ReplyTarget  — the "<nodeID>:<port>" whose inbound hop triggers the
	//     addressed reply (the origin's response port).
	//   ReplyDeadlineUnixMs — when the origin stops waiting and falls back to
	//     poll; a reply published after it is dropped (no subscriber).
	ReplySubject        string
	ReplyTarget         string
	ReplyDeadlineUnixMs int64

	seqMu sync.Mutex
	seq   map[string]int
	// emits collects the durable hops published while handling this
	// message, so the step's ledger record can carry them for re-drive.
	emits []EmitRecord
}

// NewRun wraps an existing run identity arriving on a message.
func NewRun(runID, stepKey string) *Run {
	return &Run{RunID: runID, StepKey: stepKey, seq: map[string]int{}}
}

// WithReply attaches a reply address so the run's response hop is delivered
// to the exact origin instance. Chainable; returns the same Run.
func (r *Run) WithReply(subject, target string, deadlineUnixMs int64) *Run {
	r.ReplySubject = subject
	r.ReplyTarget = target
	r.ReplyDeadlineUnixMs = deadlineUnixMs
	return r
}

// IsReplyHop reports whether an emit to targetPortAddr ("<nodeID>:<port>")
// should be delivered to the run's reply subject instead of the work-queue.
func (r *Run) IsReplyHop(targetPortAddr string) bool {
	return r.ReplySubject != "" && r.ReplyTarget != "" && targetPortAddr == r.ReplyTarget
}

// MintRun starts a new durable run at an entry node. The seed StepKey is
// distinct per run so first-hop keys can't collide across runs.
func MintRun() *Run {
	id := uuid.NewString()
	return &Run{RunID: id, StepKey: id + ".root", seq: map[string]int{}}
}

// NextStepKey derives the idempotency key for the next emit over edgeID.
// Fixed length regardless of chain depth (a hash, not an append), and
// deterministic per (parent StepKey, edgeID, per-edge emit ordinal) —
// repeated emits to the same edge within one handler invocation get 0,1,2,…,
// which is stable as long as the component's emit order is deterministic.
func (r *Run) NextStepKey(edgeID string) string {
	r.seqMu.Lock()
	n := r.seq[edgeID]
	r.seq[edgeID] = n + 1
	r.seqMu.Unlock()

	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%s|%s|%d", r.StepKey, edgeID, n)
	return fmt.Sprintf("%s.%016x", r.RunID, h.Sum64())
}

// RecordEmit notes a durable hop published while handling this message.
func (r *Run) RecordEmit(e EmitRecord) {
	r.seqMu.Lock()
	r.emits = append(r.emits, e)
	r.seqMu.Unlock()
}

// Emits returns the durable hops recorded so far.
func (r *Run) Emits() []EmitRecord {
	r.seqMu.Lock()
	defer r.seqMu.Unlock()
	return append([]EmitRecord(nil), r.emits...)
}

type runCtxKey struct{}

// WithRun attaches a run identity to the context for the duration of one
// handler invocation. Runtime machinery — components use BeginRun.
func WithRun(ctx context.Context, r *Run) context.Context {
	return context.WithValue(ctx, runCtxKey{}, r)
}

// RunFrom returns the run identity attached by WithRun, if any. Runtime
// machinery — components use RunID.
func RunFrom(ctx context.Context) (*Run, bool) {
	r, ok := ctx.Value(runCtxKey{}).(*Run)
	return r, ok
}

// BeginRun starts a durable run from within a component handler and returns
// a context carrying its identity plus the new run id. Emits made with the
// returned context are published durably (fire-and-forget with idempotency
// keys) — they return as soon as the hop is stored, without blocking on the
// downstream chain — while emits made with the ORIGINAL context keep the
// classic blocking behavior. That split is the front-door pattern:
//
//	runCtx, runID := module.BeginRun(ctx)
//	if res := output(runCtx, "out", payload); res.Err() != nil {  // durable chain
//	    return res
//	}
//	return output(ctx, "started", Started{RunID: runID})          // sync reply
//
// Idempotent: if ctx already carries a run (the component sits inside a
// durable flow), that run is returned unchanged rather than forking a new
// one.
//
// Without a JetStream transport the durable path degrades to classic
// blocking dispatch — BeginRun still returns a run id, but emits will block
// until the chain completes.
func BeginRun(ctx context.Context) (context.Context, string) {
	if r, ok := RunFrom(ctx); ok {
		return ctx, r.RunID
	}
	r := MintRun()
	return WithRun(ctx, r), r.RunID
}

// BeginRunWithReply starts a durable run whose terminal hop (the one wired to
// targetPortAddr, e.g. an http_server's "<nodeID>:response") is delivered
// straight to replySubject via core NATS — reaching the exact instance that
// started the run instead of a round-robin queue-group member. The origin
// subscribes to replySubject and holds its connection until the reply arrives
// or deadlineUnixMs passes. Used by synchronous front doors to keep a
// request open over a durable run without pinning the whole run to one pod.
//
// Idempotent like BeginRun: an existing run on ctx is returned unchanged.
func BeginRunWithReply(ctx context.Context, replySubject, targetPortAddr string, deadlineUnixMs int64) (context.Context, string) {
	if r, ok := RunFrom(ctx); ok {
		return ctx, r.RunID
	}
	r := MintRun().WithReply(replySubject, targetPortAddr, deadlineUnixMs)
	return WithRun(ctx, r), r.RunID
}

// RunID returns the durable run id carried by ctx, if any. Components use
// it to expose the run handle (e.g. returning it to an HTTP caller for
// status polling).
func RunID(ctx context.Context) (string, bool) {
	r, ok := RunFrom(ctx)
	if !ok {
		return "", false
	}
	return r.RunID, true
}
