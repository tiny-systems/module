// Package state provides backend implementations of the module.State
// interface. The metadata backend reads through the controller-runtime
// cache (continuously updated by the TinyNode watch) and writes through
// the reconcile-port debouncer.
//
// All replicas of a module see the same cache view, so multi-replica
// state convergence is automatic — no per-component "is local newer than
// metadata" flag is needed.
package state

import (
	"context"
	"encoding/base64"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// keyPrefix namespaces state keys inside TinyNode.Status.Metadata to avoid
// collisions with existing reconcile-port metadata usage (e.g., http-module's
// "http-start" and "port" keys). Component authors never see this prefix —
// state.Get("counter") reads metadata["_state/counter"] under the hood.
const keyPrefix = "_state/"

// pendingTTL bounds how long a pending entry shadows the cache. Longer
// than any reasonable watch propagation delay, short enough not to mask
// genuine concurrent writes from other replicas indefinitely.
const pendingTTL = 5 * time.Second

// EmitFunc emits a message to a port on the owning runner. The metadata
// backend uses this to publish reconcile-port updaters that patch
// TinyNode.Status.Metadata. Matches module.Handler's signature.
type EmitFunc func(ctx context.Context, port string, data any) module.Result

// pendingEntry tracks a write that hasn't yet been observed via the watch.
// value == nil means a tombstone (pending delete).
type pendingEntry struct {
	value   []byte
	addedAt time.Time
}

// MetadataState implements module.State by reading through the K8s cache
// and writing via the reconcile-port debouncer.
//
// Reads:
//   - Pending overlay (recent local writes) shadows cache to give
//     read-your-writes for the brief window between a Set and the watch
//     event reflecting it.
//   - Otherwise, read from cache via client.Reader.Get(node).
//
// Writes:
//   - Update pending overlay immediately.
//   - Emit reconcile-port updater (debounced patch through the runner).
//
// The pending overlay self-expires after pendingTTL; after that, all reads
// fall through to the cache, which by then has caught up.
type MetadataState struct {
	reader  client.Reader
	nodeKey types.NamespacedName

	mu      sync.RWMutex
	pending map[string]pendingEntry

	emit EmitFunc
}

// NewMetadataState constructs a fresh metadata-backed State that reads
// through the supplied cache reader and writes via emit.
//
// The returned State is safe for concurrent use by a single component.
// It is not designed to be shared across nodes — each runner gets its own.
func NewMetadataState(reader client.Reader, nodeKey types.NamespacedName, emit EmitFunc) *MetadataState {
	return &MetadataState{
		reader:  reader,
		nodeKey: nodeKey,
		pending: make(map[string]pendingEntry),
		emit:    emit,
	}
}

// Get returns the decoded value for key. Pending overlay wins over cache
// for the TTL window after a write.
func (m *MetadataState) Get(ctx context.Context, key string) ([]byte, bool, error) {
	m.mu.RLock()
	if entry, ok := m.pending[key]; ok && time.Since(entry.addedAt) < pendingTTL {
		m.mu.RUnlock()
		if entry.value == nil {
			return nil, false, nil
		}
		return entry.value, true, nil
	}
	m.mu.RUnlock()

	if m.reader == nil {
		return nil, false, nil
	}

	var node v1alpha1.TinyNode
	if err := m.reader.Get(ctx, m.nodeKey, &node); err != nil {
		return nil, false, err
	}

	encoded, ok := node.Status.Metadata[keyPrefix+key]
	if !ok {
		return nil, false, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, true, err
	}
	return decoded, true, nil
}

// Set updates the pending overlay and emits a reconcile-port updater that
// patches the persisted metadata.
func (m *MetadataState) Set(ctx context.Context, key string, value []byte) error {
	encoded := base64.StdEncoding.EncodeToString(value)
	fullKey := keyPrefix + key

	m.mu.Lock()
	m.pending[key] = pendingEntry{value: value, addedAt: time.Now()}
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

// Delete removes a key. Tombstones it in the pending overlay so subsequent
// Get returns absent immediately, and emits a reconcile-port updater that
// removes it from persisted metadata. Idempotent.
func (m *MetadataState) Delete(ctx context.Context, key string) error {
	fullKey := keyPrefix + key

	m.mu.Lock()
	m.pending[key] = pendingEntry{value: nil, addedAt: time.Now()}
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

// List returns sorted keys (without the internal prefix) under the given
// user-prefix. Reflects pending overlay (writes appear, tombstones hide).
func (m *MetadataState) List(ctx context.Context, prefix string) ([]string, error) {
	keys := map[string]bool{}

	if m.reader != nil {
		var node v1alpha1.TinyNode
		if err := m.reader.Get(ctx, m.nodeKey, &node); err != nil {
			return nil, err
		}
		fullPrefix := keyPrefix + prefix
		for k := range node.Status.Metadata {
			if !strings.HasPrefix(k, fullPrefix) {
				continue
			}
			keys[k[len(keyPrefix):]] = true
		}
	}

	m.mu.RLock()
	for k, entry := range m.pending {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if time.Since(entry.addedAt) >= pendingTTL {
			continue
		}
		if entry.value == nil {
			delete(keys, k)
		} else {
			keys[k] = true
		}
	}
	m.mu.RUnlock()

	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// Static interface assertion — fails to compile if drift occurs.
var _ module.State = (*MetadataState)(nil)
