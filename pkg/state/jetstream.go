package state

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// execBucket is the JetStream KV bucket that holds execution-scoped state for
// every run on this module. Keys are namespaced "exec/<runID>/<userKey>", so a
// single bucket serves all runs and any pod handling a run reads the same view.
const execBucket = "TINY_EXEC"

// EnsureExecKV creates-or-opens the execution-state KV bucket. Call it once at
// startup with the module's JetStream handle and pass the result to
// NewScopedFactory. Returns (nil, nil) when js is nil (no broker configured) so
// callers can wire it unconditionally and degrade to node-only state.
func EnsureExecKV(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	if js == nil {
		return nil, nil
	}
	// History 1: only the latest value per key is needed for durability — each
	// run turn is its own key, so no version history is required. FileStorage
	// so run state survives a broker restart (single-node today; bump replicas
	// with NATS HA later without touching this code).
	return js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  execBucket,
		Storage: jetstream.FileStorage,
		History: 1,
	})
}

// ScopedFactory produces node-local metadata State that can additionally hand
// out execution-scoped, JetStream-backed State via Scoped(module.ScopeExecution,
// runID). When kv is nil (no broker) it behaves exactly like MetadataFactory.
type ScopedFactory struct {
	reader client.Reader
	kv     jetstream.KeyValue
}

// NewScopedFactory returns a Factory backed by node-local metadata plus, when
// kv is non-nil, a durable JetStream execution scope.
func NewScopedFactory(reader client.Reader, kv jetstream.KeyValue) *ScopedFactory {
	return &ScopedFactory{reader: reader, kv: kv}
}

func (f *ScopedFactory) For(node *v1alpha1.TinyNode, emit EmitFunc) module.State {
	nodeState := NewMetadataState(f.reader, types.NamespacedName{
		Name:      node.Name,
		Namespace: node.Namespace,
	}, emit)
	if f.kv == nil {
		return nodeState
	}
	return &ScopedRouter{node: nodeState, kv: f.kv}
}

var _ Factory = (*ScopedFactory)(nil)

// ScopedRouter delegates the default (node-local) State calls to a metadata
// backend and routes Scoped(module.ScopeExecution, runID) to durable JetStream
// storage. Any other scope falls back to node-local.
type ScopedRouter struct {
	node module.State
	kv   jetstream.KeyValue
}

func (r *ScopedRouter) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return r.node.Get(ctx, key)
}
func (r *ScopedRouter) Set(ctx context.Context, key string, value []byte) error {
	return r.node.Set(ctx, key, value)
}
func (r *ScopedRouter) Delete(ctx context.Context, key string) error {
	return r.node.Delete(ctx, key)
}
func (r *ScopedRouter) List(ctx context.Context, prefix string) ([]string, error) {
	return r.node.List(ctx, prefix)
}
func (r *ScopedRouter) Scoped(scope, id string) module.State {
	if scope == module.ScopeExecution && r.kv != nil && id != "" {
		return newExecState(r.kv, id)
	}
	return r.node.Scoped(scope, id)
}

var _ module.State = (*ScopedRouter)(nil)

// JetStreamState is a module.State backed by a JetStream KV bucket, namespaced
// by ns (e.g. "exec/<runID>/"). It is durable across pod restarts and visible
// to every pod handling the run — the property node-local metadata state
// cannot give.
type JetStreamState struct {
	kv jetstream.KeyValue
	ns string // sanitized namespace, always ending in "/"
}

func newExecState(kv jetstream.KeyValue, runID string) *JetStreamState {
	return &JetStreamState{kv: kv, ns: sanitizeKey("exec/" + runID + "/")}
}

func (s *JetStreamState) full(key string) string { return s.ns + sanitizeKey(key) }

func (s *JetStreamState) Get(ctx context.Context, key string) ([]byte, bool, error) {
	e, err := s.kv.Get(ctx, s.full(key))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return e.Value(), true, nil
}

func (s *JetStreamState) Set(ctx context.Context, key string, value []byte) error {
	_, err := s.kv.Put(ctx, s.full(key), value)
	return err
}

func (s *JetStreamState) Delete(ctx context.Context, key string) error {
	err := s.kv.Delete(ctx, s.full(key))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil
	}
	return err
}

func (s *JetStreamState) List(ctx context.Context, prefix string) ([]string, error) {
	match := s.ns + sanitizeKey(prefix)
	keys, err := s.kv.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range keys {
		if strings.HasPrefix(k, match) {
			out = append(out, strings.TrimPrefix(k, s.ns))
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *JetStreamState) Scoped(scope, id string) module.State {
	if scope == module.ScopeExecution && id != "" {
		return newExecState(s.kv, id)
	}
	return s
}

var _ module.State = (*JetStreamState)(nil)

// sanitizeKey maps a key/id into the NATS KV key charset (alphanumeric plus
// - _ / . =); anything else becomes '_'. Slashes are preserved so prefix
// listing and turn-per-key layouts (turn/0, turn/1, …) work.
func sanitizeKey(k string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '/', r == '.', r == '=':
			return r
		default:
			return '_'
		}
	}, k)
}
