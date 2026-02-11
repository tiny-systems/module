package runner

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/api/v1alpha1"
	m "github.com/tiny-systems/module/module"
)

// mockComponent is a test component that can dynamically change its ports
type mockComponent struct {
	mu       sync.RWMutex
	ports    []m.Port
	portFunc func() []m.Port // Optional dynamic port function
}

func (c *mockComponent) GetInfo() m.ComponentInfo {
	return m.ComponentInfo{Name: "mock", Description: "Mock component for testing"}
}

func (c *mockComponent) Handle(ctx context.Context, output m.Handler, port string, message any) any {
	return nil
}

func (c *mockComponent) Ports() []m.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.portFunc != nil {
		return c.portFunc()
	}
	return c.ports
}

func (c *mockComponent) Instance() m.Component {
	return c
}

func (c *mockComponent) SetPorts(ports []m.Port) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ports = ports
}

// createTestRunnerWithComponent creates a runner with the given component for testing
func createTestRunnerWithComponent(component m.Component) *Runner {
	return NewRunner(component).SetLogger(logr.Discard())
}

// TestRunner_PortCache_RequiresExplicitInvalidation tests that the port cache
// is NOT automatically invalidated when component.Ports() returns different values.
// This is EXPECTED behavior - cache invalidation happens via SetNode or InvalidatePortCache.
func TestRunner_PortCache_RequiresExplicitInvalidation(t *testing.T) {
	// Create component with initial ports
	component := &mockComponent{
		ports: []m.Port{
			{Name: "input", Configuration: "config1"},
			{Name: "output", Configuration: "config2"},
		},
	}

	runner := createTestRunnerWithComponent(component)

	// First call populates cache
	ports1 := runner.getPorts()
	if len(ports1) != 2 {
		t.Fatalf("Expected 2 ports, got %d", len(ports1))
	}

	// Verify initial ports
	if !runner.HasPort("input") {
		t.Error("Expected to have 'input' port")
	}
	if !runner.HasPort("output") {
		t.Error("Expected to have 'output' port")
	}
	if runner.HasPort("_control") {
		t.Error("Should NOT have '_control' port initially")
	}

	// Update component ports (simulates internal state change)
	component.SetPorts([]m.Port{
		{Name: "input", Configuration: "config1"},
		{Name: "output", Configuration: "config2"},
		{Name: "_control", Configuration: "config3"}, // NEW PORT
	})

	// Without explicit invalidation, cache still returns old ports (expected)
	ports2 := runner.getPorts()
	if len(ports2) != 2 {
		t.Errorf("Expected cache to still return 2 ports without explicit invalidation, got %d", len(ports2))
	}

	// After explicit invalidation, new ports should be visible
	runner.InvalidatePortCache()
	ports3 := runner.getPorts()
	if len(ports3) != 3 {
		t.Errorf("Expected 3 ports after InvalidatePortCache(), got %d", len(ports3))
	}

	if !runner.HasPort("_control") {
		t.Error("Expected to have '_control' port after InvalidatePortCache()")
	}
}

// TestRunner_PortCache_InvalidatedOnSetNode tests that calling SetNode
// should invalidate the port cache so new ports become visible.
// This test EXPOSES THE BUG - SetNode does NOT invalidate the cache.
func TestRunner_PortCache_InvalidatedOnSetNode(t *testing.T) {
	// Create component with initial ports
	component := &mockComponent{
		ports: []m.Port{
			{Name: "input", Configuration: "config1"},
		},
	}

	runner := createTestRunnerWithComponent(component)

	// Populate cache
	_ = runner.getPorts()

	// Update component ports
	component.SetPorts([]m.Port{
		{Name: "input", Configuration: "config1"},
		{Name: "_control", Configuration: "config2"}, // NEW PORT
	})

	// Call SetNode (simulates node spec update from Kubernetes)
	runner.SetNode(v1alpha1.TinyNode{
		Spec: v1alpha1.TinyNodeSpec{
			Component: "mock",
			Edges: []v1alpha1.TinyNodeEdge{
				{ID: "edge1", Port: "output", To: "other:input"},
			},
		},
	})

	// After SetNode, the port cache should be invalidated
	// But currently it's NOT - this exposes the bug
	if !runner.HasPort("_control") {
		t.Errorf("BUG EXPOSED: SetNode should invalidate port cache, but '_control' port not visible")
	}
}

// TestRunner_PortCache_GetPortsReturnsConsistentSnapshot tests that getPorts
// returns a consistent snapshot even under concurrent access.
func TestRunner_PortCache_GetPortsReturnsConsistentSnapshot(t *testing.T) {
	component := &mockComponent{
		ports: []m.Port{
			{Name: "port1", Configuration: "config1"},
			{Name: "port2", Configuration: "config2"},
		},
	}

	runner := createTestRunnerWithComponent(component)

	// Populate cache
	_ = runner.getPorts()

	var wg sync.WaitGroup
	var inconsistentCount atomic.Int32

	// Concurrent readers while component changes ports
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ports := runner.getPorts()
			// All returned ports should have non-empty Name
			for _, p := range ports {
				if p.Name == "" {
					inconsistentCount.Add(1)
				}
			}
		}()
	}

	// Concurrent writer updating component ports
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			component.SetPorts([]m.Port{
				{Name: "port1", Configuration: "updated"},
				{Name: "port2", Configuration: "updated"},
				{Name: "port3", Configuration: "new"}, // Add new port
			})
		}(i)
	}

	wg.Wait()

	if inconsistentCount.Load() > 0 {
		t.Errorf("Found %d inconsistent port reads", inconsistentCount.Load())
	}
}
