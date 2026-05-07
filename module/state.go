package module

import "context"

// State is the SDK-facing storage primitive. Components hold a State (via
// the Stateful capability or via Base) and use it to persist values that
// must survive component restarts and reconciles.
//
// The default backend stores values in TinyNode.Status.Metadata. Other
// backends (Redis, Postgres, embedded BadgerDB on a PVC) are pluggable
// via the module's Requirements; the component code is identical across
// backends.
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
	Get(ctx context.Context, key string) (value []byte, ok bool, err error)

	// Set writes the value. Persistence may be asynchronous (the metadata
	// backend debounces writes through the reconcile-port path); subsequent
	// Get calls in the same component see the value immediately.
	Set(ctx context.Context, key string, value []byte) error

	// Delete removes the key. Idempotent — deleting a missing key is not
	// an error.
	Delete(ctx context.Context, key string) error
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
