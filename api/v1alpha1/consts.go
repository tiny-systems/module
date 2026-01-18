package v1alpha1

const (
	//ReconcilePort target port. Useful when component wants refresh its look in cluster, triggers reconcile for the node
	ReconcilePort = "_reconcile"
	//ControlPort dashboard
	ControlPort = "_control"
	// SettingsPort settings page
	SettingsPort = "_settings"

	//ClientPort receives a client wrapper to work with cluster resources
	ClientPort = "_client"

	// StatePort receives TinyState for this node.
	// Components use this to persist and restore runtime state (e.g., running, context).
	// Delivered on state changes and leader changes.
	StatePort = "_state"

	// BlockingStateFrom is the value of From field for blocking state messages
	// delivered by the TinyStateController via blocking TinyState
	BlockingStateFrom = "blocking"
)

// StateUpdate is passed to ReconcilePort handler to update TinyState.
// Components use this to persist their runtime state.
type StateUpdate struct {
	// Data is the JSON-encoded state data. nil means delete state.
	Data []byte
	// OwnerNode is the node name whose TinyState should own this state.
	// When the owner's TinyState is deleted, this state is cascade deleted.
	// If empty, the state is owned by its own TinyNode.
	OwnerNode string
}

// MetadataConfigPatch is passed to ReconcilePort handler to update node metadata.
// Used for legacy metadata-based state storage.
type MetadataConfigPatch struct {
	Metadata map[string]string
}
