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

	// Blocking: when true, use TinyState instead of gRPC for blocking edges.
	// The scheduler creates a TinyState for the destination node and blocks
	// until the TinyState is deleted.
	Blocking bool
}
