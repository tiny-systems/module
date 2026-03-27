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
	// IdentityPort receives node identity information (name, namespace, flow, project)
	IdentityPort = "_identity"
)

// NodeIdentity is delivered to the IdentityPort so components know their own resource name.
type NodeIdentity struct {
	NodeName    string `json:"nodeName"`
	Namespace   string `json:"namespace"`
	FlowName    string `json:"flowName"`
	ProjectName string `json:"projectName"`
}

// MetadataConfigPatch is passed to ReconcilePort handler to update node metadata.
type MetadataConfigPatch struct {
	Metadata map[string]string
}
