package state_test

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/pkg/state"
)

// recordedEmit captures emit calls so tests can verify the metadata backend
// publishes reconcile-port updaters with the expected effect on a node.
type recordedEmit struct {
	mu       sync.Mutex
	updaters []func(*v1alpha1.TinyNode) error
}

func (r *recordedEmit) Emit() state.EmitFunc {
	return func(_ context.Context, port string, data any) any {
		if port != v1alpha1.ReconcilePort {
			return nil
		}
		updater, ok := data.(func(*v1alpha1.TinyNode) error)
		if !ok {
			return nil
		}
		r.mu.Lock()
		r.updaters = append(r.updaters, updater)
		r.mu.Unlock()
		return nil
	}
}

func (r *recordedEmit) ApplyAll(node *v1alpha1.TinyNode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.updaters {
		if err := u(node); err != nil {
			return err
		}
	}
	return nil
}

func (r *recordedEmit) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.updaters)
}

func TestMetadataState_GetAbsentReturnsFalse(t *testing.T) {
	s := state.NewMetadataState(nil, nil)
	v, ok, err := s.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Get on missing key returned error: %v", err)
	}
	if ok {
		t.Errorf("Get on missing key returned ok=true; want false")
	}
	if v != nil {
		t.Errorf("Get on missing key returned value %v; want nil", v)
	}
}

func TestMetadataState_SetThenGet(t *testing.T) {
	rec := &recordedEmit{}
	s := state.NewMetadataState(nil, rec.Emit())

	if err := s.Set(context.Background(), "counter", []byte("42")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, err := s.Get(context.Background(), "counter")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Fatalf("Get returned ok=false after Set")
	}
	if string(v) != "42" {
		t.Errorf("Get returned %q; want %q", v, "42")
	}
}

func TestMetadataState_SetEmitsReconcileUpdater(t *testing.T) {
	rec := &recordedEmit{}
	s := state.NewMetadataState(nil, rec.Emit())

	if err := s.Set(context.Background(), "k", []byte("v")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if rec.Count() != 1 {
		t.Fatalf("expected 1 emitted updater; got %d", rec.Count())
	}

	// Apply the updater to a fresh node and verify metadata contains the
	// prefixed key with a base64-encoded value.
	node := &v1alpha1.TinyNode{}
	if err := rec.ApplyAll(node); err != nil {
		t.Fatalf("ApplyAll failed: %v", err)
	}
	encoded, ok := node.Status.Metadata["_state/k"]
	if !ok {
		t.Fatalf("metadata key '_state/k' not set; metadata=%+v", node.Status.Metadata)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("metadata value not base64: %v", err)
	}
	if string(decoded) != "v" {
		t.Errorf("decoded value %q; want %q", decoded, "v")
	}
}

func TestMetadataState_DeleteRemovesFromSnapshotAndEmits(t *testing.T) {
	rec := &recordedEmit{}
	initial := map[string]string{
		"_state/k": base64.StdEncoding.EncodeToString([]byte("v")),
	}
	s := state.NewMetadataState(initial, rec.Emit())

	// Sanity: present before delete.
	if _, ok, _ := s.Get(context.Background(), "k"); !ok {
		t.Fatalf("key 'k' should be present before delete")
	}

	if err := s.Delete(context.Background(), "k"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, ok, _ := s.Get(context.Background(), "k"); ok {
		t.Errorf("key 'k' should be absent after delete")
	}

	// Emitted updater should remove from a node's metadata too.
	node := &v1alpha1.TinyNode{
		Status: v1alpha1.TinyNodeStatus{
			Metadata: map[string]string{
				"_state/k": "old-value",
				"unrelated": "keep",
			},
		},
	}
	if err := rec.ApplyAll(node); err != nil {
		t.Fatalf("ApplyAll failed: %v", err)
	}
	if _, present := node.Status.Metadata["_state/k"]; present {
		t.Errorf("'_state/k' should be removed from metadata; got %+v", node.Status.Metadata)
	}
	if v, ok := node.Status.Metadata["unrelated"]; !ok || v != "keep" {
		t.Errorf("unrelated metadata key was modified; got %+v", node.Status.Metadata)
	}
}

func TestMetadataState_DeleteMissingIsNotAnError(t *testing.T) {
	s := state.NewMetadataState(nil, nil)
	if err := s.Delete(context.Background(), "ghost"); err != nil {
		t.Errorf("Delete on missing key returned error: %v", err)
	}
}

func TestMetadataState_UpdateSnapshotReplacesContents(t *testing.T) {
	s := state.NewMetadataState(map[string]string{
		"_state/old": base64.StdEncoding.EncodeToString([]byte("gone")),
	}, nil)

	s.UpdateSnapshot(map[string]string{
		"_state/new": base64.StdEncoding.EncodeToString([]byte("fresh")),
	})

	if _, ok, _ := s.Get(context.Background(), "old"); ok {
		t.Errorf("'old' key should be gone after snapshot replace")
	}
	v, ok, _ := s.Get(context.Background(), "new")
	if !ok || string(v) != "fresh" {
		t.Errorf("'new' key not present after snapshot replace; ok=%v, v=%q", ok, v)
	}
}

func TestMetadataState_UpdateSnapshotNilClears(t *testing.T) {
	s := state.NewMetadataState(map[string]string{
		"_state/x": base64.StdEncoding.EncodeToString([]byte("y")),
	}, nil)

	s.UpdateSnapshot(nil)

	if _, ok, _ := s.Get(context.Background(), "x"); ok {
		t.Errorf("snapshot should be empty after UpdateSnapshot(nil)")
	}
}

func TestMetadataState_NilEmitDoesNotPanic(t *testing.T) {
	s := state.NewMetadataState(nil, nil)
	if err := s.Set(context.Background(), "k", []byte("v")); err != nil {
		t.Errorf("Set with nil emit returned error: %v", err)
	}
	if err := s.Delete(context.Background(), "k"); err != nil {
		t.Errorf("Delete with nil emit returned error: %v", err)
	}
}

// TestMetadataFactory_ProducesStateSeededFromNode verifies the factory's
// initial-snapshot wiring: the state for a node should see that node's
// metadata immediately on first Get.
func TestMetadataFactory_ProducesStateSeededFromNode(t *testing.T) {
	node := &v1alpha1.TinyNode{
		Status: v1alpha1.TinyNodeStatus{
			Metadata: map[string]string{
				"_state/seeded": base64.StdEncoding.EncodeToString([]byte("hello")),
			},
		},
	}
	f := state.NewMetadataFactory()
	s := f.For(node, nil)

	v, ok, err := s.Get(context.Background(), "seeded")
	if err != nil {
		t.Fatalf("Get on factory-seeded state failed: %v", err)
	}
	if !ok {
		t.Fatalf("seeded key should be present")
	}
	if string(v) != "hello" {
		t.Errorf("Get returned %q; want %q", v, "hello")
	}
}
