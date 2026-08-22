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

// Runtimes that can execute a TinyComponent definition.
//
// A module implementing one registers it in init(), the same way it registers
// components. The SDK's controller looks the factory up by the definition's
// runtime name, so adding a second language is a factory and nothing else — no
// controller, no CRD handling, no reconciliation logic to get wrong twice.
var runtimeFactories = map[string]module.RuntimeFactory{}

// RegisterRuntime declares that this module can execute definitions naming
// `runtime`. Registering the same name twice is last-wins, which only happens
// if one binary links two implementations of one language.
func RegisterRuntime(runtime string, f module.RuntimeFactory) {
	if runtime == "" || f == nil {
		return
	}
	runtimeFactories[runtime] = f
}

// GetRuntime returns the factory for a runtime, if this module implements it.
//
// A definition naming a runtime nobody implements is NOT an error: the module
// that serves it may simply not be installed. The caller reports that as an
// uninstalled component with a reason, so a flow referencing it fails visibly
// rather than the definition sitting inert and looking fine.
func GetRuntime(runtime string) (module.RuntimeFactory, bool) {
	f, ok := runtimeFactories[runtime]
	return f, ok
}

// Runtimes lists the runtime names this module implements.
func Runtimes() []string {
	out := make([]string, 0, len(runtimeFactories))
	for k := range runtimeFactories {
		out = append(out, k)
	}
	return out
}
