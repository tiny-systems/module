package module

// Requirements defines module-level requirements for deployment
type Requirements struct {
	RBAC    RBACRequirements    `json:"rbac,omitempty"`
	Storage StorageRequirements `json:"storage,omitempty"`
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
