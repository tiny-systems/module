package module

import "context"

// State is the SDK-facing storage primitive. Components hold a State (via
// the Stateful capability or via Base) and use it to persist values that
// must survive component restarts and reconciles.
//
// The default backend reads through the controller-runtime cache (which
// is continuously updated by the TinyNode watch) and writes through the
// reconcile-port debouncer. All replicas of a module observe the same
// cache state via watch, so they converge automatically without
// per-component flag bookkeeping.
//
// Keys are scoped to the node by default. Future scopes (execution, flow,
// project) will arrive as separate methods (Scoped(...)) on State; for
// now only node scope exists.
//
// Values are opaque []byte. Callers serialize/deserialize as needed
// (encoding/json, gob, etc).
type State interface {
	// Get returns the value for key. The bool indicates presence; an
	// absent key returns (nil, false, nil) — not an error.
	//
	// Reads are eventually consistent across replicas (cache is updated
	// by the K8s watch). Read-your-writes is preserved within the same
	// component instance for a short window after Set.
	Get(ctx context.Context, key string) (value []byte, ok bool, err error)

	// Set writes the value. The patch is debounced and applied via the
	// reconcile-port. Subsequent Get calls on the same component see the
	// value immediately (via the pending-write overlay); other replicas
	// observe it after the patch commits and the watch event propagates.
	Set(ctx context.Context, key string, value []byte) error

	// Delete removes the key. Idempotent — deleting a missing key is not
	// an error. Same propagation semantics as Set.
	Delete(ctx context.Context, key string) error

	// List returns all keys under the given prefix (without the State's
	// own internal prefix). Order is sorted ascending. Reflects pending
	// writes and deletes from this replica.
	List(ctx context.Context, prefix string) ([]string, error)
}

// Stateful is the capability interface for components that want a State
// backend injected. The framework calls OnState once during runner setup,
// before OnReconcile and OnSettings. Components without state needs simply
// don't implement this.
//
// Most components will get this for free by embedding Base.
type Stateful interface {
	OnState(s State)
}
