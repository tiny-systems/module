package registry

import (
	"github.com/tiny-systems/module/module"
)

var defaultCollection []module.Component

func Register(c module.Component) {
	defaultCollection = append(defaultCollection, c)
}

func Get() []module.Component {
	return defaultCollection
}
