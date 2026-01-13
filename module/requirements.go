package module

// Requirements defines module-level requirements for deployment
type Requirements struct {
	RBAC RBACRequirements `json:"rbac,omitempty"`
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
