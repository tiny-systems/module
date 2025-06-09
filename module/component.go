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

type Component interface {
	GetInfo() ComponentInfo
	//Handle handles incoming requests
	Handle(ctx context.Context, output Handler, port string, message any) any
	//Ports gets list of ports
	Ports() []Port
	//Instance creates new instance with default settings
	Instance() Component
}
