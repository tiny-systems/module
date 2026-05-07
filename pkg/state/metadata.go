// Package state provides backend implementations of the module.State
// interface. The metadata backend persists values via the existing
// TinyNode.Status.Metadata reconcile-port pattern, keeping write traffic
// inside the platform's existing K8s API budget.
package state

import (
	"context"
	"encoding/base64"
	"sync"

	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
)

// keyPrefix namespaces state keys inside TinyNode.Status.Metadata to avoid
// collisions with existing reconcile-port metadata usage (e.g., http-module's
// "http-start" and "port" keys). Component authors never see this prefix —
// state.Get("counter") reads metadata["_state/counter"] under the hood.
const keyPrefix = "_state/"

// EmitFunc emits a message to a port on the owning runner. The metadata
// backend uses this to publish reconcile-port updaters that patch
// TinyNode.Status.Metadata. Matches module.Handler's signature.
type EmitFunc func(ctx context.Context, port string, data any) any

// MetadataState implements module.State by storing values in
// TinyNode.Status.Metadata. Reads are served from an in-memory snapshot
// kept in sync with each reconcile; writes update the snapshot immediately
// and are flushed to the K8s API via the reconcile-port debouncer.
//
// MetadataState is safe for concurrent use by a single component. It is
// not designed to be shared across nodes — each runner gets its own.
type MetadataState struct {
	mu       sync.RWMutex
	snapshot map[string]string
	emit     EmitFunc
}

// NewMetadataState constructs a fresh metadata-backed State. The initial
// snapshot is taken from the supplied map; emit is the function used to
// publish reconcile-port updaters when Set or Delete are called.
//
// Pass nil for emit in tests or when persistence is intentionally disabled
// (rare); Get/Set/Delete still operate on the in-memory snapshot.
func NewMetadataState(initial map[string]string, emit EmitFunc) *MetadataState {
	snap := make(map[string]string, len(initial))
	for k, v := range initial {
		snap[k] = v
	}
	return &MetadataState{snapshot: snap, emit: emit}
}

// Get returns the decoded value for key. Absent keys yield (nil, false, nil).
func (m *MetadataState) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	encoded, ok := m.snapshot[keyPrefix+key]
	if !ok {
		return nil, false, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, true, err
	}
	return decoded, true, nil
}

// Set updates the in-memory snapshot and emits a reconcile-port updater
// that patches the persisted metadata. Subsequent Get calls in the same
// component see the value immediately, regardless of when the K8s patch
// commits.
func (m *MetadataState) Set(ctx context.Context, key string, value []byte) error {
	encoded := base64.StdEncoding.EncodeToString(value)
	fullKey := keyPrefix + key

	m.mu.Lock()
	m.snapshot[fullKey] = encoded
	m.mu.Unlock()

	if m.emit == nil {
		return nil
	}

	updater := func(node *v1alpha1.TinyNode) error {
		if node.Status.Metadata == nil {
			node.Status.Metadata = map[string]string{}
		}
		node.Status.Metadata[fullKey] = encoded
		return nil
	}
	m.emit(ctx, v1alpha1.ReconcilePort, updater)
	return nil
}

// Delete removes a key from the snapshot and emits an updater that removes
// it from persisted metadata. Idempotent.
func (m *MetadataState) Delete(ctx context.Context, key string) error {
	fullKey := keyPrefix + key

	m.mu.Lock()
	delete(m.snapshot, fullKey)
	m.mu.Unlock()

	if m.emit == nil {
		return nil
	}

	updater := func(node *v1alpha1.TinyNode) error {
		if node.Status.Metadata == nil {
			return nil
		}
		delete(node.Status.Metadata, fullKey)
		return nil
	}
	m.emit(ctx, v1alpha1.ReconcilePort, updater)
	return nil
}

// UpdateSnapshot replaces the in-memory snapshot with a copy of metadata.
// Called by the runner after each reconcile so subsequent Get calls reflect
// any externally-applied changes (e.g., from a different leader).
//
// This method is not part of module.State because backends like Redis or
// Postgres don't need it. The runner type-asserts on it.
func (m *MetadataState) UpdateSnapshot(metadata map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if metadata == nil {
		m.snapshot = map[string]string{}
		return
	}
	m.snapshot = make(map[string]string, len(metadata))
	for k, v := range metadata {
		m.snapshot[k] = v
	}
}

// Static interface assertion — fails to compile if drift occurs.
var _ module.State = (*MetadataState)(nil)
