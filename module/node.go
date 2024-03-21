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
	//HttpPort source port. Provides addressUpgrader
	HttpPort = "_http"
)

const (
	Top Position = iota
	Right
	Bottom
	Left
)

type NodePort struct {
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
