package module

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client interface for components that need ExposePort/DisclosePort
// Deprecated: Use K8sClient interface instead and implement port management locally
type Client interface {
	ExposePort(ctx context.Context, autoHostName string, hostnames []string, port int) ([]string, error)
	DisclosePort(ctx context.Context, port int) error
}

// K8sClient provides access to the raw Kubernetes client for modules
// that need to interact with K8s resources directly
type K8sClient interface {
	GetK8sClient() client.WithWatch
	GetNamespace() string
}
