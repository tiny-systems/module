package runner

// Msg being sent via instances edges
type Msg struct {
	EdgeID  string
	Subject string
	Data    interface{}
	Err     error
}
