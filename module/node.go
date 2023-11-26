package module

type (
	Position int
)

const (
	SaveStatePort string = "_save-state"
	GetStatePort         = "_get-state"
	SettingsPort         = "_settings"
	StatusPort           = "_status"
	RunPort              = "_run"
	StopPort             = "_stop"
)

const (
	Top Position = iota
	Right
	Bottom
	Left
)

type NodePort struct {
	Source        bool
	Status        bool
	Settings      bool
	Position      Position
	Name          string
	Label         string
	Configuration interface{}
}
