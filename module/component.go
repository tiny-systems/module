package module

import (
	"context"
)

type ComponentInfo struct {
	Name        string
	Description string
	Info        string
	Tags        []string
}

type Component interface {
	GetInfo() ComponentInfo
	// Handle processes a message arriving on `port`. Components emit downstream
	// (or to system ports) by calling `output`. Both `output`'s return and the
	// return of Handle itself are Result; chain them up the call stack so
	// blocking-I/O callers (http-server etc.) receive the synchronous response
	// that flows back from downstream nodes. Construct Result via Ok / Fail.
	Handle(ctx context.Context, output Handler, port string, message any) Result
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
