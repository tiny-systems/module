package state_test

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/state"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNS   = "default"
	testName = "test-node"
)

// nodeKey returns the standard NamespacedName used by all tests.
func nodeKey() types.NamespacedName {
	return types.NamespacedName{Namespace: testNS, Name: testName}
}

// newFakeClient returns a controller-runtime fake client seeded with a
// TinyNode whose status.metadata starts as the supplied map.
func newFakeClient(t *testing.T, initial map[string]string) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	node := &v1alpha1.TinyNode{}
	node.Name = testName
	node.Namespace = testNS
	node.Status.Metadata = initial
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(node).
		WithStatusSubresource(&v1alpha1.TinyNode{}).
		Build()
}

// recordedEmit captures emit calls so tests can verify the metadata backend
// publishes reconcile-port updaters with the expected effect on a node.
type recordedEmit struct {
	mu       sync.Mutex
	updaters []func(*v1alpha1.TinyNode) error
}

func (r *recordedEmit) Emit() state.EmitFunc {
	return func(_ context.Context, port string, data any) module.Result {
		if port != v1alpha1.ReconcilePort {
			return module.Result{}
		}
		updater, ok := data.(func(*v1alpha1.TinyNode) error)
		if !ok {
			return module.Result{}
		}
		r.mu.Lock()
		r.updaters = append(r.updaters, updater)
		r.mu.Unlock()
		return module.Result{}
	}
}

// ApplyAll runs the captured updaters against a node, returning the result.
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
	c := newFakeClient(t, nil)
	s := state.NewMetadataState(c, nodeKey(), nil)
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

func TestMetadataState_GetReadsFromCache(t *testing.T) {
	c := newFakeClient(t, map[string]string{
		"_state/seeded": base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	s := state.NewMetadataState(c, nodeKey(), nil)

	v, ok, err := s.Get(context.Background(), "seeded")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if !ok {
		t.Fatalf("Get returned ok=false")
	}
	if string(v) != "hello" {
		t.Errorf("Get returned %q; want %q", v, "hello")
	}
}

func TestMetadataState_SetThenGetUsesPendingOverlay(t *testing.T) {
	// Cache starts empty; Set should make Get return the value immediately
	// without waiting for the cache to update.
	c := newFakeClient(t, nil)
	rec := &recordedEmit{}
	s := state.NewMetadataState(c, nodeKey(), rec.Emit())

	if err := s.Set(context.Background(), "counter", []byte("42")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, err := s.Get(context.Background(), "counter")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Fatalf("Get returned ok=false after Set; pending overlay should have caught it")
	}
	if string(v) != "42" {
		t.Errorf("Get returned %q; want %q", v, "42")
	}
}

func TestMetadataState_SetEmitsReconcileUpdater(t *testing.T) {
	c := newFakeClient(t, nil)
	rec := &recordedEmit{}
	s := state.NewMetadataState(c, nodeKey(), rec.Emit())

	if err := s.Set(context.Background(), "k", []byte("v")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if rec.Count() != 1 {
		t.Fatalf("expected 1 emitted updater; got %d", rec.Count())
	}

	// Apply the updater and verify it puts the right key+base64-value
	// into status.metadata.
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

func TestMetadataState_DeleteTombstonesPending(t *testing.T) {
	// Cache has the key; Delete should make Get return absent immediately
	// even before the cache catches up.
	c := newFakeClient(t, map[string]string{
		"_state/k": base64.StdEncoding.EncodeToString([]byte("v")),
	})
	rec := &recordedEmit{}
	s := state.NewMetadataState(c, nodeKey(), rec.Emit())

	// Sanity: present before delete.
	if _, ok, _ := s.Get(context.Background(), "k"); !ok {
		t.Fatalf("key 'k' should be present before delete")
	}

	if err := s.Delete(context.Background(), "k"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Pending tombstone should hide the cached value.
	if _, ok, _ := s.Get(context.Background(), "k"); ok {
		t.Errorf("key 'k' should be absent after delete (tombstone in pending)")
	}
}

func TestMetadataState_DeleteEmitsRemovalUpdater(t *testing.T) {
	c := newFakeClient(t, nil)
	rec := &recordedEmit{}
	s := state.NewMetadataState(c, nodeKey(), rec.Emit())

	if err := s.Delete(context.Background(), "k"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	node := &v1alpha1.TinyNode{
		Status: v1alpha1.TinyNodeStatus{
			Metadata: map[string]string{
				"_state/k":  "old-value",
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
	c := newFakeClient(t, nil)
	s := state.NewMetadataState(c, nodeKey(), nil)
	if err := s.Delete(context.Background(), "ghost"); err != nil {
		t.Errorf("Delete on missing key returned error: %v", err)
	}
}

func TestMetadataState_NilEmitDoesNotPanic(t *testing.T) {
	c := newFakeClient(t, nil)
	s := state.NewMetadataState(c, nodeKey(), nil)
	if err := s.Set(context.Background(), "k", []byte("v")); err != nil {
		t.Errorf("Set with nil emit returned error: %v", err)
	}
	if err := s.Delete(context.Background(), "k"); err != nil {
		t.Errorf("Delete with nil emit returned error: %v", err)
	}
}

func TestMetadataState_NilReaderReturnsAbsent(t *testing.T) {
	s := state.NewMetadataState(nil, nodeKey(), nil)
	v, ok, err := s.Get(context.Background(), "k")
	if err != nil {
		t.Errorf("Get with nil reader returned error: %v", err)
	}
	if ok {
		t.Errorf("Get with nil reader returned ok=true; want false")
	}
	if v != nil {
		t.Errorf("Get with nil reader returned non-nil value")
	}
}

func TestMetadataState_ListIncludesCacheAndPending(t *testing.T) {
	// Cache has two keys, we set a third and delete one.
	c := newFakeClient(t, map[string]string{
		"_state/a":     base64.StdEncoding.EncodeToString([]byte("av")),
		"_state/b":     base64.StdEncoding.EncodeToString([]byte("bv")),
		"unrelated/x":  "keep-out",
		"_state/other": base64.StdEncoding.EncodeToString([]byte("ov")),
	})
	rec := &recordedEmit{}
	s := state.NewMetadataState(c, nodeKey(), rec.Emit())

	// Add a new key under the same prefix and tombstone an existing one.
	if err := s.Set(context.Background(), "c", []byte("cv")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Delete(context.Background(), "a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	keys, err := s.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := map[string]bool{"b": true, "c": true, "other": true}
	if len(keys) != len(want) {
		t.Errorf("List returned %d keys; want %d. got=%v", len(keys), len(want), keys)
	}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("unexpected key in List: %q", k)
		}
	}
}

func TestMetadataState_ListWithPrefixFilters(t *testing.T) {
	c := newFakeClient(t, map[string]string{
		"_state/foo-a":    base64.StdEncoding.EncodeToString([]byte("1")),
		"_state/foo-b":    base64.StdEncoding.EncodeToString([]byte("2")),
		"_state/bar-a":    base64.StdEncoding.EncodeToString([]byte("3")),
	})
	s := state.NewMetadataState(c, nodeKey(), nil)

	keys, err := s.List(context.Background(), "foo-")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 foo-* keys; got %d (%v)", len(keys), keys)
	}
	for _, k := range keys {
		if k != "foo-a" && k != "foo-b" {
			t.Errorf("unexpected key %q in foo- prefix list", k)
		}
	}
}

// TestMetadataFactory_ProducesStateScopedToNode verifies the factory
// constructs state with the right node identity.
func TestMetadataFactory_ProducesStateScopedToNode(t *testing.T) {
	c := newFakeClient(t, map[string]string{
		"_state/seeded": base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	node := &v1alpha1.TinyNode{}
	node.Name = testName
	node.Namespace = testNS

	f := state.NewMetadataFactory(c)
	s := f.For(node, nil)

	v, ok, err := s.Get(context.Background(), "seeded")
	if err != nil {
		t.Fatalf("Get on factory-scoped state failed: %v", err)
	}
	if !ok {
		t.Fatalf("seeded key should be present")
	}
	if string(v) != "hello" {
		t.Errorf("Get returned %q; want %q", v, "hello")
	}
}

// --- Size-guard tests --------------------------------------------

// TestMetadataState_SetUnderCapSucceeds is the happy path: a write
// well under MaxStateBytes goes through without complaint.
func TestMetadataState_SetUnderCapSucceeds(t *testing.T) {
	c := newFakeClient(t, nil)
	rec := &recordedEmit{}
	s := state.NewMetadataState(c, nodeKey(), rec.Emit())

	if err := s.Set(context.Background(), "small", []byte("just-a-few-bytes")); err != nil {
		t.Fatalf("Set under cap failed: %v", err)
	}
	if rec.Count() != 1 {
		t.Errorf("expected 1 emit, got %d", rec.Count())
	}
}

// TestMetadataState_SetOverCapRejectsAsTooLarge proves the guard
// fires before any reconcile-port emit when the write would push
// total _state/* size over MaxStateBytes.
func TestMetadataState_SetOverCapRejectsAsTooLarge(t *testing.T) {
	c := newFakeClient(t, nil)
	rec := &recordedEmit{}
	s := state.NewMetadataState(c, nodeKey(), rec.Emit())

	// Write a value larger than the cap. Base64 inflation alone makes
	// MaxStateBytes worth of raw bytes blow through after encoding.
	oversized := make([]byte, state.MaxStateBytes+1)
	err := s.Set(context.Background(), "blob", oversized)
	if err == nil {
		t.Fatalf("expected ErrStateTooLarge, got nil")
	}
	if !errorsIs(err, state.ErrStateTooLarge) {
		t.Errorf("expected error to wrap ErrStateTooLarge; got: %v", err)
	}
	if rec.Count() != 0 {
		t.Errorf("rejected write should not emit; got %d emits", rec.Count())
	}
}

// TestMetadataState_SetAccountsForExistingKeyOnReplace ensures the
// guard treats a replacement as "swap value, not add" — replacing a
// 100-byte value with a 200-byte value should only delta by 100, not
// reject because of the full 200 added on top.
func TestMetadataState_SetAccountsForExistingKeyOnReplace(t *testing.T) {
	// Seed near the cap (most of MaxStateBytes already used).
	bigPayload := make([]byte, state.MaxStateBytes*3/4)
	c := newFakeClient(t, map[string]string{
		"_state/existing": base64.StdEncoding.EncodeToString(bigPayload),
	})
	rec := &recordedEmit{}
	s := state.NewMetadataState(c, nodeKey(), rec.Emit())

	// Replacing the same key with a slightly smaller payload should
	// always succeed even though the total without replacement
	// accounting would seem over the cap.
	smallerPayload := make([]byte, state.MaxStateBytes/4)
	if err := s.Set(context.Background(), "existing", smallerPayload); err != nil {
		t.Fatalf("replacement under net cap failed: %v", err)
	}
}

// TestMetadataState_SetWithoutReaderSkipsGuard documents the
// no-reader behaviour: when no controller-runtime cache is wired
// (e.g. test fixtures, standalone evaluators), the guard no-ops.
// Production runners always have a reader.
func TestMetadataState_SetWithoutReaderSkipsGuard(t *testing.T) {
	rec := &recordedEmit{}
	s := state.NewMetadataState(nil, nodeKey(), rec.Emit())

	huge := make([]byte, state.MaxStateBytes*2)
	if err := s.Set(context.Background(), "k", huge); err != nil {
		t.Fatalf("Set without reader should not fail: %v", err)
	}
	if rec.Count() != 1 {
		t.Errorf("expected 1 emit, got %d", rec.Count())
	}
}

// errorsIs is a local stand-in to avoid importing the errors pkg
// solely for one test helper; keeps the import block tight.
func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
