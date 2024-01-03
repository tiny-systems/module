package module

import (
	"context"
	"github.com/tiny-systems/module/pkg/utils"
)

type ComponentInfo struct {
	Name        string
	Description string
	Info        string
	Tags        []string
}

func (c ComponentInfo) GetResourceName() string {
	return utils.SanitizeResourceName(c.Name)
}

//type Emitter interface {
//  Emit(ctx context.Context, handler Handler) error
//}

type Component interface {
	GetInfo() ComponentInfo
	//Handle handles incoming requests
	Handle(ctx context.Context, output Handler, port string, message interface{}) error
	//Ports gets list of ports
	Ports() []NodePort
	//Instance creates new instance with default settings
	Instance() Component
}

// StatefulComponent WIP
type StatefulComponent interface {
	GetState() ([]byte, error)
	SetState(state []byte) error
}

type AddressUpgrade func(ctx context.Context, auto bool, hostnames []string, actualLocalPort int) ([]string, error)

// ListenAddressGetter gets listen port address
type ListenAddressGetter func() (suggestedPort int, upgrade AddressUpgrade)

type HTTPService interface {
	HTTPService(getter ListenAddressGetter)
}
