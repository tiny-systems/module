package resource

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwarder manages port-forwarding to Kubernetes services
type PortForwarder struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
	namespace string

	mu         sync.Mutex
	forwarders map[string]*activeForwarder
}

// activeForwarder represents an active port-forward connection
type activeForwarder struct {
	localPort int
	stopChan  chan struct{}
	readyChan chan struct{}
	errChan   chan error
}

// CreatePortForwarderFromConfig creates a port forwarder from a rest config
func CreatePortForwarderFromConfig(config *rest.Config, namespace string) (*PortForwarder, error) {
	if config == nil {
		return nil, fmt.Errorf("rest config is nil")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &PortForwarder{
		config:     config,
		clientset:  clientset,
		namespace:  namespace,
		forwarders: make(map[string]*activeForwarder),
	}, nil
}

// ForwardService creates a port-forward to a service and returns the local address
func (pf *PortForwarder) ForwardService(ctx context.Context, serviceName string, servicePort int) (string, error) {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	key := fmt.Sprintf("%s/%s:%d", pf.namespace, serviceName, servicePort)

	// Check if we already have an active forwarder
	if af, exists := pf.forwarders[key]; exists {
		select {
		case <-af.stopChan:
			delete(pf.forwarders, key)
		default:
			return fmt.Sprintf("localhost:%d", af.localPort), nil
		}
	}

	// Find a pod backing the service
	podName, err := pf.findPodForService(ctx, serviceName)
	if err != nil {
		return "", fmt.Errorf("failed to find pod for service %s: %w", serviceName, err)
	}

	// Find a free local port
	localPort, err := getFreePort()
	if err != nil {
		return "", fmt.Errorf("failed to get free port: %w", err)
	}

	// Create port-forward
	af, err := pf.createForwarder(ctx, podName, localPort, servicePort)
	if err != nil {
		return "", fmt.Errorf("failed to create port-forward: %w", err)
	}

	pf.forwarders[key] = af
	return fmt.Sprintf("localhost:%d", localPort), nil
}

// findPodForService finds a running pod that backs a service
func (pf *PortForwarder) findPodForService(ctx context.Context, serviceName string) (string, error) {
	svc, err := pf.clientset.CoreV1().Services(pf.namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get service: %w", err)
	}

	if len(svc.Spec.Selector) == 0 {
		return "", fmt.Errorf("service %s has no selector", serviceName)
	}

	var selectorParts []string
	for k, v := range svc.Spec.Selector {
		selectorParts = append(selectorParts, fmt.Sprintf("%s=%s", k, v))
	}
	labelSelector := strings.Join(selectorParts, ",")

	pods, err := pf.clientset.CoreV1().Pods(pf.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			return pod.Name, nil
		}
	}

	return "", fmt.Errorf("no running pods found for service %s", serviceName)
}

// createForwarder creates and starts a port-forward
func (pf *PortForwarder) createForwarder(ctx context.Context, podName string, localPort, remotePort int) (*activeForwarder, error) {
	reqURL := pf.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pf.namespace).
		Name(podName).
		SubResource("portforward").URL()

	transport, upgrader, err := spdy.RoundTripperFor(pf.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", reqURL)

	stopChan := make(chan struct{})
	readyChan := make(chan struct{})
	errChan := make(chan error, 1)

	ports := []string{fmt.Sprintf("%d:%d", localPort, remotePort)}

	fw, err := portforward.New(dialer, ports, stopChan, readyChan, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create port-forwarder: %w", err)
	}

	af := &activeForwarder{
		localPort: localPort,
		stopChan:  stopChan,
		readyChan: readyChan,
		errChan:   errChan,
	}

	go func() {
		if err := fw.ForwardPorts(); err != nil {
			errChan <- err
		}
	}()

	select {
	case <-readyChan:
		return af, nil
	case err := <-errChan:
		return nil, fmt.Errorf("port-forward failed: %w", err)
	case <-ctx.Done():
		close(stopChan)
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		close(stopChan)
		return nil, fmt.Errorf("timeout waiting for port-forward")
	}
}

// StopAll stops all active port-forwards
func (pf *PortForwarder) StopAll() {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	for key, af := range pf.forwarders {
		close(af.stopChan)
		delete(pf.forwarders, key)
	}
}

// getFreePort finds a free TCP port
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}
