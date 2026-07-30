package module

import "encoding/json"

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

	// Schema, when non-nil, is published as this port's JSON schema verbatim
	// instead of reflecting Configuration. Use it for forms whose shape is
	// only known at runtime — a component that receives a schema as data and
	// presents it to a human has no compile-time Go type to reflect.
	//
	// Components with a Go type for their port keep returning Configuration
	// alone; reflection stays the default and is unaffected. Varying the shape
	// by returning a different Go type per state (as ticker does with
	// ControlRunning/ControlStopped) also keeps working — this is only for the
	// case where no Go type exists at all.
	//
	// Raw bytes rather than a decoded map so the schema passes through
	// unparsed, and because the destination field is already []byte.
	//
	// Do NOT rely on key order to order the rendered fields: a configurable
	// overlay re-encodes the node and sorts its keys, so the order written here
	// is not the order displayed. Set `propertyOrder` on each field — that is
	// what the editor sorts on.
	Schema json.RawMessage
}
