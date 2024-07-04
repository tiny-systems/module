package module

import "context"

type Client interface {
	ExposePort(ctx context.Context, autoHostName string, hostnames []string, port int) ([]string, error)
	DisclosePort(ctx context.Context, port int) error
}
