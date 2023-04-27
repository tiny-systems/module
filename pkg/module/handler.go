package module

type Handler func(port string, data interface{}) error
