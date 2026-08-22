package module

// A component defined by a resource rather than by Go source.
//
// The SDK owns the resource and the controller that watches it; a module owns
// the ability to RUN one. js-module can execute JavaScript, wasm-module can
// execute a wasm binary, and neither needs to know how definitions arrive or
// when they change. So a module registers a factory for the runtime it
// implements, and the controller calls it.
//
// Keeping the split here means a second runtime is a factory and nothing else —
// no controller, no CRD handling, no reconciliation to get wrong twice.

// ComponentDefinition is a TinyComponent reduced to what a runtime needs.
//
// Deliberately not the CRD type: a module would otherwise depend on the API
// group to implement a factory, and the point of a runtime is that it knows
// about scripts, not about Kubernetes.
type ComponentDefinition struct {
	// Name is what flows reference as TinyNode.Spec.Component.
	Name string

	Description string
	Info        string

	// Script is the implementation, in whatever language the runtime executes.
	Script string

	// InputSchema and OutputSchema are the declared port shapes. Both are
	// required by every runtime: an edge cannot be validated against a shape
	// nobody declared, in either direction.
	InputSchema  []byte
	OutputSchema []byte

	// EnableErrorPort exposes an `error` port so a caller can catch failures
	// rather than letting them abort the run.
	EnableErrorPort bool

	// TimeoutSeconds caps one invocation. Zero means the runtime's default.
	TimeoutSeconds int
}

// RuntimeFactory turns a definition into a runnable component.
//
// It must fail rather than defer: a script that will never compile should be
// reported when the resource is applied, while somebody is looking at it, not
// on the first message hours later in the middle of real traffic.
type RuntimeFactory func(ComponentDefinition) (Component, error)
