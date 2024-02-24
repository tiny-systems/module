package module

type (
	Position int
)

const (
	SaveStatePort string = "_save-state"
	GetStatePort         = "_get-state"
	SettingsPort         = "_settings"
	RefreshPort          = "_refresh"
	HttpPort             = "_http"
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
	// this port's DTO will be shown as a control panel
	Control bool
	// port with Settings equals true will be shown in configuration tab
	Settings bool
	// which side of the node will have this port
	Position Position
	// Name lower case programmatic name
	Name string
	// Human readable name (capital cased)
	Label string
	// DTO object
	Configuration interface{}
}
