package module

type (
	Position int
)

// system ports
const (
	//ReconcilePort target port. Useful when component wants refresh its look in cluster, triggers reconcile for the node
	ReconcilePort = "_reconcile"
	//ControlPort dashboard
	ControlPort = "_control"
	// SettingsPort settings page
	SettingsPort = "_settings"

	//ClientPort receives a client wrapper to work with cluster resources
	ClientPort = "_client"
)

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
}
