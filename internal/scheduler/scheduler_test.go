package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/resource"
	"github.com/tiny-systems/module/pkg/state"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeManager is a minimal resource.ManagerInterface that satisfies
// module.K8sClient too, so ClientAware components see a real client.
type fakeManager struct{}

func (fakeManager) CreateModule(_ context.Context, _ module.Info) error                    { return nil }
func (fakeManager) PatchNode(_ context.Context, _ v1alpha1.TinyNode, updater func(node *v1alpha1.TinyNode) error) error {
	// Apply the updater against a fresh node so debounced patches don't
	// blow up; tests don't assert on what's persisted here.
	if updater == nil {
		return nil
	}
	return updater(&v1alpha1.TinyNode{})
}
func (fakeManager) CreateNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (fakeManager) UpdateNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (fakeManager) DeleteNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (fakeManager) GetNode(_ context.Context, _, _ string) (*v1alpha1.TinyNode, error) { return nil, nil }
func (fakeManager) CreateSignal(_ context.Context, _, _, _ string, _ []byte, _ ...string) error {
	return nil
}
func (fakeManager) PersistDurableSignal(_ context.Context, _, _, _, _ string, _ []byte, _, _ string) error {
	return nil
}

// GetK8sClient + GetNamespace satisfy module.K8sClient. Returning nil for
// the client is fine because no test exercises actual K8s calls.
func (fakeManager) GetK8sClient() client.WithWatch { return nil }
func (fakeManager) GetNamespace() string           { return "test-ns" }

var (
	_ resource.ManagerInterface = fakeManager{}
	_ module.K8sClient          = fakeManager{}
)

// fullCapabilityComponent implements every capability interface and records
// the order in which the framework calls them. Used to assert
// scheduler.Update enforces the documented dispatch order.
type fullCapabilityComponent struct {
	mu       sync.Mutex
	order    []string
	settings any

	// State is captured to verify Stateful injection ran exactly once.
	state         module.State
	stateInjected atomic.Int32
}

func (c *fullCapabilityComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: "fullcap", Description: "test"}
}

func (c *fullCapabilityComponent) Handle(_ context.Context, _ module.Handler, _ string, _ any) module.Result {
	// Should never be called for system ports because every capability is
	// implemented. Tests assert this by checking handleCalls.
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order = append(c.order, "Handle")
	return module.Result{}
}

func (c *fullCapabilityComponent) Ports() []module.Port {
	return []module.Port{
		{Name: v1alpha1.IdentityPort},
		{Name: v1alpha1.ClientPort},
		{Name: v1alpha1.ReconcilePort},
		{Name: v1alpha1.SettingsPort, Configuration: struct{}{}},
	}
}

func (c *fullCapabilityComponent) Instance() module.Component {
	return &fullCapabilityComponent{}
}

func (c *fullCapabilityComponent) OnIdentity(_ v1alpha1.NodeIdentity) {
	c.mu.Lock()
	c.order = append(c.order, "OnIdentity")
	c.mu.Unlock()
}

func (c *fullCapabilityComponent) OnClient(_ module.K8sClient) {
	c.mu.Lock()
	c.order = append(c.order, "OnClient")
	c.mu.Unlock()
}

func (c *fullCapabilityComponent) OnState(s module.State) {
	c.state = s
	c.stateInjected.Add(1)
	c.mu.Lock()
	c.order = append(c.order, "OnState")
	c.mu.Unlock()
}

func (c *fullCapabilityComponent) OnReconcile(_ context.Context, _ v1alpha1.TinyNode) error {
	c.mu.Lock()
	c.order = append(c.order, "OnReconcile")
	c.mu.Unlock()
	return nil
}

func (c *fullCapabilityComponent) OnSettings(_ context.Context, settings any) error {
	c.mu.Lock()
	c.order = append(c.order, "OnSettings")
	c.settings = settings
	c.mu.Unlock()
	return nil
}

func (c *fullCapabilityComponent) recordedOrder() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]string, len(c.order))
	copy(cp, c.order)
	return cp
}

// newTestSchedule wires up a Schedule with all required dependencies
// stubbed for in-process tests. msgHandler returns nil for everything.
func newTestSchedule(t *testing.T) *Schedule {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.TinyNode{}).
		Build()
	s := New(func(_ context.Context, _ *runner.Msg) (any, error) { return nil, nil }).
		SetLogger(logr.Discard()).
		SetManager(fakeManager{}).
		SetTracer(tracenoop.NewTracerProvider().Tracer("test")).
		SetMeter(metricnoop.NewMeterProvider().Meter("test")).
		SetStateFactory(state.NewMetadataFactory(fakeClient))
	return s
}

// TestUpdate_DispatchesCapabilitiesInFrameworkOrder is the headline test:
// a fresh component gets OnIdentity → OnClient → OnState → OnReconcile →
// OnSettings, regardless of how its Ports() are ordered.
func TestUpdate_DispatchesCapabilitiesInFrameworkOrder(t *testing.T) {
	s := newTestSchedule(t)

	template := &fullCapabilityComponent{}
	if err := s.Install(template); err != nil {
		t.Fatalf("Install: %v", err)
	}

	node := &v1alpha1.TinyNode{}
	node.Name = "node-1"
	node.Namespace = "default"
	node.Spec.Component = "fullcap"
	node.Spec.Module = "test-module"

	if err := s.Update(context.Background(), node); err != nil {
		t.Fatalf("Update: %v", err)
	}

	r, ok := s.instancesMap.Get(node.Name)
	if !ok {
		t.Fatalf("no runner registered for %s", node.Name)
	}
	cmp, ok := r.GetComponent().(*fullCapabilityComponent)
	if !ok {
		t.Fatalf("component is %T; want *fullCapabilityComponent", r.GetComponent())
	}

	got := cmp.recordedOrder()
	want := []string{"OnIdentity", "OnClient", "OnState", "OnReconcile", "OnSettings"}

	if len(got) != len(want) {
		t.Fatalf("order length = %d; want %d. got=%v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("order[%d] = %q; want %q. full=%v", i, got[i], w, got)
		}
	}

	// Handle must NOT have been called for any system port — every system
	// port has a capability handler, so the legacy fallback is unreachable.
	for _, step := range got {
		if step == "Handle" {
			t.Errorf("Handle was called; expected only capability methods. order=%v", got)
		}
	}

	// State injection happens exactly once on creation.
	if injected := cmp.stateInjected.Load(); injected != 1 {
		t.Errorf("OnState called %d times; want 1", injected)
	}
}

// TestUpdate_StateInjectedOnlyOnFirstUpdate verifies that subsequent calls
// to Update with the same node don't re-inject state. The component holds
// the original backend across reconciles.
func TestUpdate_StateInjectedOnlyOnFirstUpdate(t *testing.T) {
	s := newTestSchedule(t)

	if err := s.Install(&fullCapabilityComponent{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	node := &v1alpha1.TinyNode{}
	node.Name = "node-2"
	node.Namespace = "default"
	node.Spec.Component = "fullcap"

	for i := 0; i < 3; i++ {
		if err := s.Update(context.Background(), node); err != nil {
			t.Fatalf("Update iteration %d: %v", i, err)
		}
	}

	r, _ := s.instancesMap.Get(node.Name)
	cmp := r.GetComponent().(*fullCapabilityComponent)

	if got := cmp.stateInjected.Load(); got != 1 {
		t.Errorf("state injected %d times across 3 Updates; want 1", got)
	}
	if cmp.state == nil {
		t.Errorf("state should be non-nil after Update")
	}
}

// noCapabilityComponent is a Component with no capability interfaces. It
// records every Handle invocation, so tests can verify Handle is NEVER
// called for system ports under the no-fallback contract.
type noCapabilityComponent struct {
	mu          sync.Mutex
	handleCalls []string
}

func (c *noCapabilityComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: "nocap", Description: "test"}
}

func (c *noCapabilityComponent) Handle(_ context.Context, _ module.Handler, port string, _ any) module.Result {
	c.mu.Lock()
	c.handleCalls = append(c.handleCalls, port)
	c.mu.Unlock()
	return module.Result{}
}

func (c *noCapabilityComponent) Ports() []module.Port {
	return []module.Port{
		{Name: v1alpha1.IdentityPort},
		{Name: v1alpha1.ClientPort},
		{Name: v1alpha1.ReconcilePort},
		{Name: v1alpha1.SettingsPort, Configuration: struct{}{}},
	}
}

func (c *noCapabilityComponent) Instance() module.Component {
	return &noCapabilityComponent{}
}

func (c *noCapabilityComponent) recordedHandleCalls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]string, len(c.handleCalls))
	copy(cp, c.handleCalls)
	return cp
}

// TestUpdate_NoCapabilityNeverInvokesHandleForSystemPorts is the no-fallback
// contract: a Component without capability interfaces gets ZERO Handle
// invocations for any system port. The component is silently skipped — if
// it needed lifecycle callbacks it should have implemented the interfaces.
func TestUpdate_NoCapabilityNeverInvokesHandleForSystemPorts(t *testing.T) {
	s := newTestSchedule(t)

	if err := s.Install(&noCapabilityComponent{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	node := &v1alpha1.TinyNode{}
	node.Name = "node-nocap"
	node.Namespace = "default"
	node.Spec.Component = "nocap"

	if err := s.Update(context.Background(), node); err != nil {
		t.Fatalf("Update: %v", err)
	}

	r, _ := s.instancesMap.Get(node.Name)
	cmp := r.GetComponent().(*noCapabilityComponent)

	got := cmp.recordedHandleCalls()
	if len(got) != 0 {
		t.Errorf("Handle was called %d times for system ports; want 0. ports=%v", len(got), got)
	}
}
