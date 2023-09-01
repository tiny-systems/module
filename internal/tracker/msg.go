package tracker

type PortMsg struct {
	NodeName  string
	EdgeID    string
	PortName  string
	FlowID    string
	Data      []byte
	NodeStats map[string]interface{}
	Err       error
}

type Callback func(msg PortMsg)
