package module

// Requirements defines module-level requirements for deployment
type Requirements struct {
	RBAC    RBACRequirements    `json:"rbac,omitempty"`
	Storage StorageRequirements `json:"storage,omitempty"`
	Bundles Bundles             `json:"bundles,omitempty"`
	Secrets SecretRequirements  `json:"secrets,omitempty"`
}

// SecretRequirements declares the k8s Secrets a module needs to read.
// The platform install flow renders a Role with resourceNames pinned
// to the names the user supplies at install time; the chart only
// grants get/list/watch on those exact Secrets in the release
// namespace. Module code resolves `{{secret:<name>/<key>}}`
// placeholders in node `_settings` via pkg/secret.Resolve.
type SecretRequirements struct {
	// Names lists the Secret names this module may reference. The
	// install UI prompts the user to supply the actual Secret names
	// (which the user creates via kubectl); the chart Role's
	// resourceNames is pinned to those values. Empty means the
	// module does not consume any Secrets and no Role is created.
	Names []string `json:"names,omitempty"`
}

// Bundles is the list of third-party Helm releases a module offers to
// provision alongside itself at install time. Authors declare these
// via registry.SetRequirements; the platform's install UI renders a
// checkbox per bundle and emits an additional helm install per
// enabled entry.
type Bundles []Bundle

// Bundle describes one provisioned-on-install Helm release. Values
// carry author defaults; ExposedSchema declares which keys appear as
// editable fields on the install form (uses the same schema-driven
// renderer the flow editor already uses for node configs).
//
// ValuesYAML is an escape hatch for charts whose values are awkward
// to express as a Go map. When set it's merged BEFORE the Values
// map at install time, so structured Values entries override raw
// YAML keys.
type Bundle struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	ChartRepo      string         `json:"chartRepo"`
	ChartName      string         `json:"chartName"`
	ChartVersion   string         `json:"chartVersion,omitempty"`
	DefaultEnabled bool           `json:"defaultEnabled,omitempty"`
	ConnectionHint string         `json:"connectionHint,omitempty"`
	Values         map[string]any `json:"values,omitempty"`
	ExposedSchema  map[string]any `json:"exposedSchema,omitempty"`
	ValuesYAML     string         `json:"valuesYAML,omitempty"`
}

// StorageRequirements defines persistent storage needs for the module
type StorageRequirements struct {
	// Enabled requests a PVC to be created and mounted for this module
	Enabled bool `json:"enabled,omitempty"`
	// Size is the requested storage size (e.g., "1Gi", "10Gi")
	Size string `json:"size,omitempty"`
	// StorageClassName is the optional storage class (uses cluster default if empty)
	StorageClassName string `json:"storageClassName,omitempty"`
}

// RBACRequirements defines RBAC permissions needed by the module
type RBACRequirements struct {
	// EnableKubernetesResourceAccess enables access to pods, services, deployments, ingresses
	EnableKubernetesResourceAccess bool `json:"enableKubernetesResourceAccess,omitempty"`
	// ExtraRules defines additional RBAC rules needed by the module
	ExtraRules []RBACRule `json:"extraRules,omitempty"`
}

// RBACRule defines a single RBAC rule
type RBACRule struct {
	APIGroups []string `json:"apiGroups,omitempty"`
	Resources []string `json:"resources,omitempty"`
	Verbs     []string `json:"verbs,omitempty"`
}
