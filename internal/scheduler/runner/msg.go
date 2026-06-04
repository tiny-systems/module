package runner

// Msg being sent via instances edges
type Msg struct {

	// which edge lead this message, optional
	EdgeID string `json:"edgeID"`
	// which node:port sent message, optional
	From string `json:"from"`
	// recipient of this message in a format node:port
	To   string `json:"to"`
	Data []byte `json:"data"`

	// Depth tracks how many node hops this message has traversed.
	// Used to detect cycles and prevent stack overflow in blocking I/O chains.
	Depth int `json:"depth"`

	// Mode picks the dispatch path. "" (default) routes to a node
	// instance via the edge graph the way every business hop has
	// since the SDK was built. "rpc" short-circuits the graph: the
	// scheduler looks up the component by name, instantiates it
	// fresh, delivers Data on the component's AgentTool InputPort,
	// captures the first emit on OutputPort, and returns that as
	// the reply. RPC mode is how MCP tool calls reach a component
	// without flow assembly.
	Mode string `json:"mode,omitempty"`

	//
	Resp interface{} `json:"-"`
}
