package module

// AgentToolInfo describes how a component is exposed as an MCP tool
// to LLM agents. Empty fields fall back to sensible defaults derived
// from the component itself, so most opted-in components only need
// to provide a short Description override (or nothing at all).
type AgentToolInfo struct {
	// Name overrides the tool name. Empty = "<module>_<component>"
	// constructed by the platform-side MCP registry; component
	// authors rarely override this.
	Name string

	// Description for the agent-facing tool. Empty = component's
	// ComponentInfo.Info text. Aim for one sentence the LLM can
	// match against natural-language requests.
	Description string

	// InputPort names the port the agent's payload arrives on for
	// RPC dispatch. Empty = "in". The Configuration field on that
	// port's struct is used as the JSON Schema the agent's tool
	// call validates against.
	InputPort string

	// OutputPort is the port the RPC dispatcher waits on for a
	// reply emit before returning to the caller. Empty = "out".
	// Components that fan out to multiple ports must pick one
	// authoritative reply port here.
	OutputPort string
}

// AgentTool is the opt-in capability components implement to be
// exposed as MCP tools. Without it, a component stays a flow-only
// building block (which is the right answer for stateful pieces:
// http_server, ticker, cron, kv).
//
// The dispatcher creates a fresh component Instance() per call, hands
// it the payload on InputPort, captures the first emit on OutputPort,
// and replies. No flow context, no edges, no settings persistence —
// the agent provides everything the component needs in the call
// payload.
//
// Only stateless components should opt in. Pure transforms
// (json_encode, transform), one-shot calls (http_request, llm_complete,
// slack_send_message, database_query) are good candidates. Anything
// that needs settings, persistent state, or a lifecycle (servers,
// timers, in-memory caches) should leave this interface unimplemented.
type AgentTool interface {
	AgentTool() AgentToolInfo
}
