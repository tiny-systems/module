package runner

// Msg being sent via instances edges
type Msg struct {
	EdgeID string
	From   string
	To     string
	Data   []byte
}
