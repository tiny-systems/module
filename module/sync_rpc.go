package module

// SyncRPCInfo is reserved for future refinements of the synchronous
// contract (per-port granularity, reply deadlines). Empty today — the
// capability itself is the signal.
type SyncRPCInfo struct{}

// SyncRPC is the opt-in capability for components that BLOCK on a
// response: they emit on a source port and synchronously wait for the
// downstream chain to deliver a result back to one of their target
// ports within the same request (http_server holding a live HTTP
// connection is the canonical case).
//
// Why it matters: durable delivery is fire-and-forget — a hop that has
// been persisted to the work-queue returns nothing to its sender. Any
// node on a sync component's request→response path must therefore run
// CLASSIC (core request/reply, which also load-balances across the
// module's pods — the RPC scaling path). The platform reads this
// capability from the published component tags and keeps the connected
// subgraph containing such a component classic; disconnected,
// trigger-driven subgraphs run durable.
//
// This replaces the old flow-level execution-mode decision: component
// authors declare what their communication NEEDS, and nobody chooses
// modes — not users, not flows.
type SyncRPC interface {
	SyncRPC() SyncRPCInfo
}
