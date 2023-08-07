package module

type (
	Position int
)

const (
	SaveStatePort string = "_save-state"
	GetStatePort  string = "_get-state"
	SettingsPort  string = "_settings"
	StatusPort    string = "_status"
)

const (
	Top Position = iota
	Right
	Bottom
	Left
)

type NodePort struct {
	Source   bool
	Status   bool
	Settings bool
	Position Position
	Name     string
	Label    string
	Message  interface{}
}
