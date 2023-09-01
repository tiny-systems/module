package runner

var EmptyCallback = func(err error) {}

// Msg being sent via instances edges
type Msg struct {
	// which edge lead this message, optional
	EdgeID string `json:"edgeID"`
	// which node:port sent message, optional
	From string `json:"from"`
	// recipient of this message in a format node:port
	To string `json:"to"`

	Data []byte `json:"data"`
	// meta data like flowID and etc, optional
	//Meta map[string]string `json:"meta"`

	Callback func(error) `json:"-"` // to track if msg was rejected
}
