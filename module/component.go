package module

import (
	"context"
)

type ComponentInfo struct {
	Name        string
	Description string
	Info        string
	Tags        []string
	// Examples are short usage snippets showing how to wire this
	// component in common flows. Surfaced via MCP tooling so LLM
	// callers can copy a known-good pattern instead of deriving one.
	Examples []string
}

type Component interface {
	GetInfo() ComponentInfo
	//Handle handles incoming requests
	Handle(ctx context.Context, output Handler, port string, message any) any
	//Ports gets list of ports
	Ports() []Port
	//Instance creates new instance with default settings
	Instance() Component
}

// Destroyer is an optional interface that components can implement
// to clean up resources when the node is being destroyed (e.g., on finalizer).
// The metadata map contains any persistent state stored in node.Status.Metadata.
type Destroyer interface {
	OnDestroy(metadata map[string]string)
}
