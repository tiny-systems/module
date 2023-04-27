package module

type (
	Position int
)

const (
	SaveStatePort string = "_save-state"
	GetStatePort  string = "_get-state"
	ConfigurePort string = "_configure"
	RunPort       string = "_run"
	StopPort      string = "_stop"
	DestroyPort   string = "_destroy"
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

func GetPortByName(ports []NodePort, name string) *NodePort {
	for _, p := range ports {
		if p.Name == name {
			return &p
		}
	}
	return nil
}
