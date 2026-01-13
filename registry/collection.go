package registry

import (
	"github.com/tiny-systems/module/module"
)

var defaultCollection []module.Component
var moduleRequirements *module.Requirements

func Register(c module.Component) {
	defaultCollection = append(defaultCollection, c)
}

func Get() []module.Component {
	return defaultCollection
}

// SetRequirements sets module-level requirements (RBAC, etc.)
func SetRequirements(r module.Requirements) {
	moduleRequirements = &r
}

// GetRequirements returns module-level requirements
func GetRequirements() *module.Requirements {
	return moduleRequirements
}
