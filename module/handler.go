package module

import "context"

type Handler func(ctx context.Context, port string, data interface{}) error
