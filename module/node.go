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

	//NodePort source port. Received tinynode object
	NodePort = "_node"

	ClientPort = "_client"
)

const (
	Top Position = iota
	Right
	Bottom
	Left
)

type Port struct {
	// if that's a source port, source means it accepts the data, the source of incoming data
	Source bool
	// which side of the node will have this port
	Position Position
	// Name lower case programmatic name
	Name string
	// Human readable name (capital cased)
	Label string
	// DTO object
	Configuration interface{}
}
