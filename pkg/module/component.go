package module

import (
	"context"
)

type ComponentInfo struct {
	Name        string
	Description string
	Info        string
	Tags        []string
}

type Runnable interface {
	Run(ctx context.Context, handler Handler) error
}

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

type ListenAddressGetter func(port int) (public string, err error)

type HTTPService interface {
	HTTPService(getter ListenAddressGetter)
}
