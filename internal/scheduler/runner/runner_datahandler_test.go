package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/api/v1alpha1"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/resource"
)

// mockManager implements resource.ManagerInterface for testing.
// It captures the updater function passed to PatchNode so tests can inspect
// what would have been written to K8s.
type mockManager struct {
	mu             sync.Mutex
	patchCallCount int
	lastUpdater    func(node *v1alpha1.TinyNode) error
}

func (m *mockManager) PatchNode(_ context.Context, _ v1alpha1.TinyNode, updater func(node *v1alpha1.TinyNode) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.patchCallCount++
	m.lastUpdater = updater
	// Execute the updater against a fresh node to simulate K8s behavior
	node := &v1alpha1.TinyNode{
		Status: v1alpha1.TinyNodeStatus{
			Metadata: map[string]string{
				"some-running": "true",
				"some-config":  `{"key":"value"}`,
			},
		},
	}
	return updater(node)
}

func (m *mockManager) CreateModule(_ context.Context, _ m.Info) error                    { return nil }
func (m *mockManager) CreateNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (m *mockManager) UpdateNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (m *mockManager) DeleteNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (m *mockManager) GetNode(_ context.Context, _, _ string) (*v1alpha1.TinyNode, error) { return nil, nil }
func (m *mockManager) CreateSignal(_ context.Context, _, _, _ string, _ []byte) error    { return nil }

func (m *mockManager) getPatchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.patchCallCount
}

var _ resource.ManagerInterface = (*mockManager)(nil)

// patchCapturingManager is like mockManager but captures the node state AFTER the updater runs.
type patchCapturingManager struct {
	mu          sync.Mutex
	patchCount  int
	lastNode    *v1alpha1.TinyNode // node state after updater applied
	initialNode v1alpha1.TinyNode  // initial node state to feed to updater
}

func (m *patchCapturingManager) PatchNode(_ context.Context, _ v1alpha1.TinyNode, updater func(node *v1alpha1.TinyNode) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.patchCount++
	node := m.initialNode.DeepCopy()
	if err := updater(node); err != nil {
		return err
	}
	m.lastNode = node
	return nil
}

func (m *patchCapturingManager) getLastNode() *v1alpha1.TinyNode {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastNode == nil {
		return nil
	}
	return m.lastNode.DeepCopy()
}

func (m *patchCapturingManager) getPatchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.patchCount
}

func (m *patchCapturingManager) CreateModule(_ context.Context, _ m.Info) error                    { return nil }
func (m *patchCapturingManager) CreateNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (m *patchCapturingManager) UpdateNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (m *patchCapturingManager) DeleteNode(_ context.Context, _ *v1alpha1.TinyNode) error          { return nil }
func (m *patchCapturingManager) GetNode(_ context.Context, _, _ string) (*v1alpha1.TinyNode, error) { return nil, nil }
func (m *patchCapturingManager) CreateSignal(_ context.Context, _, _, _ string, _ []byte) error    { return nil }

var _ resource.ManagerInterface = (*patchCapturingManager)(nil)

// createTestRunnerWithManager creates a runner with a mock component and mock manager.
func createTestRunnerWithManager(mgr resource.ManagerInterface) *Runner {
	cmp := &mockComponent{
		ports: []m.Port{
			{Name: v1alpha1.ControlPort, Label: "Control", Configuration: struct{ Status string }{Status: "OK"}},
			{Name: v1alpha1.SettingsPort, Label: "Settings", Configuration: struct{}{}},
		},
	}
	r := NewRunner(cmp).
		SetLogger(logr.Discard()).
		SetManager(mgr)
	return r
}

// noopHandler is a handler that does nothing (used when DataHandler's outputHandler is not exercised).
func noopHandler(_ context.Context, _ *Msg) (any, error) {
	return nil, nil
}

// TestDataHandler_ControlPortDrainsPendingUpdaters verifies that when a ControlPort
// debounce replaces a pending ReconcilePort debounce, the accumulated
// pendingNodeUpdaters are still applied.
//
// This is the exact sequence that occurs when a component stops:
// 1. clearMetadata → ReconcilePort → accumulates updater, debounce starts timer
// 2. emit defer → ControlPort → replaces debounced function
// 3. Timer fires → ControlPort function must drain pendingNodeUpdaters
func TestDataHandler_ControlPortDrainsPendingUpdaters(t *testing.T) {
	mgr := &patchCapturingManager{
		initialNode: v1alpha1.TinyNode{
			Status: v1alpha1.TinyNodeStatus{
				Metadata: map[string]string{
					"some-running": "true",
					"some-config":  `{"key":"value"}`,
				},
			},
		},
	}

	// Use a short debounce for testing
	r := createTestRunnerWithManager(mgr)
	r.reconcileDebouncer = NewReconcileDebouncer(100 * time.Millisecond)

	handler := r.DataHandler(noopHandler)
	ctx := context.Background()

	// Step 1: ReconcilePort with a metadata-clearing updater
	handler(ctx, v1alpha1.ReconcilePort, func(n *v1alpha1.TinyNode) error {
		if n.Status.Metadata == nil {
			return nil
		}
		delete(n.Status.Metadata, "some-running")
		delete(n.Status.Metadata, "some-config")
		return nil
	})

	// Step 2: ControlPort replaces the debounced function (like emit defer does)
	handler(ctx, v1alpha1.ControlPort, struct{ Status string }{Status: "Stopped"})

	// Step 3: Wait for debounce to fire
	time.Sleep(300 * time.Millisecond)

	// Verify: PatchNode was called
	if mgr.getPatchCount() == 0 {
		t.Fatal("PatchNode was never called — debounce didn't fire")
	}

	// Verify: metadata was cleared by the updater
	node := mgr.getLastNode()
	if node == nil {
		t.Fatal("lastNode is nil — PatchNode updater wasn't captured")
	}

	if _, exists := node.Status.Metadata["some-running"]; exists {
		t.Error("metadata key 'some-running' still exists — pendingNodeUpdaters were NOT drained by ControlPort handler")
	}
	if _, exists := node.Status.Metadata["some-config"]; exists {
		t.Error("metadata key 'some-config' still exists — pendingNodeUpdaters were NOT drained by ControlPort handler")
	}
}

// TestDataHandler_ReconcilePortDrainsPendingUpdaters verifies the normal case:
// ReconcilePort debounce fires and drains its own pendingNodeUpdaters.
func TestDataHandler_ReconcilePortDrainsPendingUpdaters(t *testing.T) {
	mgr := &patchCapturingManager{
		initialNode: v1alpha1.TinyNode{
			Status: v1alpha1.TinyNodeStatus{
				Metadata: map[string]string{
					"some-running": "true",
					"some-config":  `{"key":"value"}`,
				},
			},
		},
	}

	r := createTestRunnerWithManager(mgr)
	r.reconcileDebouncer = NewReconcileDebouncer(100 * time.Millisecond)

	handler := r.DataHandler(noopHandler)
	ctx := context.Background()

	// ReconcilePort with a metadata-clearing updater (NO ControlPort to replace it)
	handler(ctx, v1alpha1.ReconcilePort, func(n *v1alpha1.TinyNode) error {
		if n.Status.Metadata == nil {
			return nil
		}
		delete(n.Status.Metadata, "some-running")
		delete(n.Status.Metadata, "some-config")
		return nil
	})

	// Wait for debounce
	time.Sleep(300 * time.Millisecond)

	if mgr.getPatchCount() == 0 {
		t.Fatal("PatchNode was never called")
	}

	node := mgr.getLastNode()
	if node == nil {
		t.Fatal("lastNode is nil")
	}

	if _, exists := node.Status.Metadata["some-running"]; exists {
		t.Error("metadata key 'some-running' still exists after ReconcilePort drain")
	}
	if _, exists := node.Status.Metadata["some-config"]; exists {
		t.Error("metadata key 'some-config' still exists after ReconcilePort drain")
	}
}

// TestDataHandler_MultipleUpdatersBatched verifies that multiple ReconcilePort calls
// within the debounce window have all their updaters applied in a single PatchNode call.
func TestDataHandler_MultipleUpdatersBatched(t *testing.T) {
	mgr := &patchCapturingManager{
		initialNode: v1alpha1.TinyNode{
			Status: v1alpha1.TinyNodeStatus{
				Metadata: map[string]string{},
			},
		},
	}

	r := createTestRunnerWithManager(mgr)
	r.reconcileDebouncer = NewReconcileDebouncer(100 * time.Millisecond)

	handler := r.DataHandler(noopHandler)
	ctx := context.Background()

	// Three ReconcilePort calls in rapid succession
	handler(ctx, v1alpha1.ReconcilePort, func(n *v1alpha1.TinyNode) error {
		if n.Status.Metadata == nil {
			n.Status.Metadata = make(map[string]string)
		}
		n.Status.Metadata["key1"] = "value1"
		return nil
	})
	handler(ctx, v1alpha1.ReconcilePort, func(n *v1alpha1.TinyNode) error {
		if n.Status.Metadata == nil {
			n.Status.Metadata = make(map[string]string)
		}
		n.Status.Metadata["key2"] = "value2"
		return nil
	})
	handler(ctx, v1alpha1.ReconcilePort, func(n *v1alpha1.TinyNode) error {
		if n.Status.Metadata == nil {
			n.Status.Metadata = make(map[string]string)
		}
		n.Status.Metadata["key3"] = "value3"
		return nil
	})

	// Wait for single debounce
	time.Sleep(300 * time.Millisecond)

	// Should be exactly 1 PatchNode call (all batched)
	if count := mgr.getPatchCount(); count != 1 {
		t.Errorf("expected 1 PatchNode call (batched), got %d", count)
	}

	node := mgr.getLastNode()
	if node == nil {
		t.Fatal("lastNode is nil")
	}

	for _, key := range []string{"key1", "key2", "key3"} {
		if _, exists := node.Status.Metadata[key]; !exists {
			t.Errorf("metadata key %q missing — updater was not applied", key)
		}
	}
}

// TestDataHandler_StopLifecycle_FullSequence simulates the complete stop lifecycle
// as it happens in production:
//
//   Ticker handleControl(Stop):
//     1. clearMetadata → handler(ReconcilePort, deleteFunc)
//     2. t.stop() → cancels emit goroutine
//   Emit goroutine defer:
//     3. handler(ControlPort, stoppedStatus)
//
// The ControlPort debounce REPLACES the ReconcilePort debounce.
// The test verifies that metadata is cleared despite the replacement.
func TestDataHandler_StopLifecycle_FullSequence(t *testing.T) {
	mgr := &patchCapturingManager{
		initialNode: v1alpha1.TinyNode{
			Status: v1alpha1.TinyNodeStatus{
				Metadata: map[string]string{
					"ticker-running": "true",
					"ticker-config":  `{"delay":1000}`,
				},
			},
		},
	}

	r := createTestRunnerWithManager(mgr)
	r.reconcileDebouncer = NewReconcileDebouncer(100 * time.Millisecond)

	handler := r.DataHandler(noopHandler)
	ctx := context.Background()

	// Step 1: clearMetadata (from handleControl)
	handler(ctx, v1alpha1.ReconcilePort, func(n *v1alpha1.TinyNode) error {
		if n.Status.Metadata == nil {
			return nil
		}
		delete(n.Status.Metadata, "ticker-running")
		delete(n.Status.Metadata, "ticker-config")
		return nil
	})

	// Step 2: ControlPort "Stopped" (from emit goroutine defer, runs concurrently)
	// Simulates short delay for goroutine scheduling
	time.Sleep(5 * time.Millisecond)
	handler(ctx, v1alpha1.ControlPort, struct{ Status string }{Status: "Not running"})

	// Wait for debounce to fire
	time.Sleep(300 * time.Millisecond)

	if mgr.getPatchCount() == 0 {
		t.Fatal("PatchNode was never called")
	}

	node := mgr.getLastNode()
	if node == nil {
		t.Fatal("lastNode is nil")
	}

	if _, exists := node.Status.Metadata["ticker-running"]; exists {
		t.Error("STOP LIFECYCLE BUG: 'ticker-running' metadata NOT cleared — " +
			"ControlPort debounce replaced ReconcilePort debounce without draining pendingNodeUpdaters")
	}
	if _, exists := node.Status.Metadata["ticker-config"]; exists {
		t.Error("STOP LIFECYCLE BUG: 'ticker-config' metadata NOT cleared")
	}
}

// TestDataHandler_ControlPortWithoutPendingUpdaters verifies that ControlPort
// works correctly even when there are no pending updaters to drain.
func TestDataHandler_ControlPortWithoutPendingUpdaters(t *testing.T) {
	mgr := &patchCapturingManager{
		initialNode: v1alpha1.TinyNode{
			Status: v1alpha1.TinyNodeStatus{
				Metadata: map[string]string{
					"keep-this": "value",
				},
			},
		},
	}

	r := createTestRunnerWithManager(mgr)
	r.reconcileDebouncer = NewReconcileDebouncer(100 * time.Millisecond)

	handler := r.DataHandler(noopHandler)
	ctx := context.Background()

	// Only ControlPort, no prior ReconcilePort
	handler(ctx, v1alpha1.ControlPort, struct{ Status string }{Status: "Running"})

	time.Sleep(300 * time.Millisecond)

	if mgr.getPatchCount() == 0 {
		t.Fatal("PatchNode was never called")
	}

	node := mgr.getLastNode()
	if node == nil {
		t.Fatal("lastNode is nil")
	}

	// Metadata should be untouched (no updaters to drain)
	if v, exists := node.Status.Metadata["keep-this"]; !exists || v != "value" {
		t.Error("metadata was unexpectedly modified when there were no pending updaters")
	}
}

// TestDebouncer_ReplacedFunctionExecutes verifies that when Debounce is called twice
// within the interval, only the SECOND function is executed.
func TestDebouncer_ReplacedFunctionExecutes(t *testing.T) {
	d := NewReconcileDebouncer(100 * time.Millisecond)

	var firstCalled, secondCalled bool
	var mu sync.Mutex

	ctx := context.Background()

	d.Debounce(ctx, func() {
		mu.Lock()
		firstCalled = true
		mu.Unlock()
	})

	d.Debounce(ctx, func() {
		mu.Lock()
		secondCalled = true
		mu.Unlock()
	})

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if firstCalled {
		t.Error("first function was called — should have been replaced")
	}
	if !secondCalled {
		t.Error("second (replacement) function was NOT called")
	}
}

// TestDebouncer_Flush verifies that Flush immediately executes pending updates.
func TestDebouncer_Flush(t *testing.T) {
	d := NewReconcileDebouncer(10 * time.Second) // very long interval

	var called bool
	var mu sync.Mutex

	d.Debounce(context.Background(), func() {
		mu.Lock()
		called = true
		mu.Unlock()
	})

	// Flush should execute immediately
	d.Flush()

	mu.Lock()
	defer mu.Unlock()

	if !called {
		t.Error("Flush did not execute the pending update")
	}
}
