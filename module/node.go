package module

type (
	Position int
)

// system ports

const (
	Top Position = iota
	Right
	Bottom
	Left
)

type Port struct {
	// if that's a source port, source means it can be a source of data
	Source bool
	// which side of the node will have this port
	Position Position
	// Name lower case programmatic name
	Name string

	// Human readable name (capital cased)
	Label string
	// Request conf
	Configuration interface{}

	// Response conf
	ResponseConfiguration interface{}

	// Durable marks this port as requiring persistent intake. Messages
	// arriving on a durable port are written as TinySignal CRDs before
	// dispatch and delivered asynchronously by the TinySignal reconciler.
	// The caller's gRPC ack returns as soon as the CRD is persisted —
	// it does NOT block on the component's handler completing.
	//
	// Use this for ports whose work must survive a pod crash mid-handle:
	// payment processing, notification sends, side effects that cost
	// money or move data. Components that read durable port input must
	// be idempotent on the deterministic signal name.
	//
	// Default false (gRPC fast-path, blocking-I/O semantics).
	Durable bool
}
