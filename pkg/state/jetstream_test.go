package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/tiny-systems/module/module"
)

// startJS boots an embedded nats-server with JetStream and returns a handle.
func startJS(t *testing.T) jetstream.JetStream {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = -1
	opts.JetStream = true
	opts.StoreDir = filepath.Join(t.TempDir(), "js")
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(2 * time.Second) {
		t.Fatal("nats/JS not ready")
	}
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream.New: %v", err)
	}
	return js
}

func execKV(t *testing.T) jetstream.KeyValue {
	t.Helper()
	kv, err := EnsureExecKV(context.Background(), startJS(t))
	if err != nil {
		t.Fatalf("EnsureExecKV: %v", err)
	}
	if kv == nil {
		t.Fatal("EnsureExecKV returned nil with a live JS")
	}
	return kv
}

func TestEnsureExecKV_NilJS(t *testing.T) {
	kv, err := EnsureExecKV(context.Background(), nil)
	if err != nil || kv != nil {
		t.Fatalf("nil JS should yield (nil,nil); got (%v,%v)", kv, err)
	}
}

func TestJetStreamState_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newExecState(execKV(t), "run-1")

	// missing key
	if v, ok, err := s.Get(ctx, "turn/0"); err != nil || ok || v != nil {
		t.Fatalf("missing key should be (nil,false,nil); got (%v,%v,%v)", v, ok, err)
	}
	// set + get
	if err := s.Set(ctx, "turn/0", []byte("hello")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok, err := s.Get(ctx, "turn/0"); err != nil || !ok || string(v) != "hello" {
		t.Fatalf("Get after Set: (%q,%v,%v)", v, ok, err)
	}
	// list under prefix (internal namespace stripped)
	_ = s.Set(ctx, "turn/1", []byte("world"))
	_ = s.Set(ctx, "meta/status", []byte("running"))
	keys, err := s.List(ctx, "turn")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 2 || keys[0] != "turn/0" || keys[1] != "turn/1" {
		t.Fatalf("List(turn) = %v, want [turn/0 turn/1]", keys)
	}
	// delete is idempotent
	if err := s.Delete(ctx, "turn/0"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete(ctx, "turn/0"); err != nil {
		t.Fatalf("Delete missing should be nil; got %v", err)
	}
	if _, ok, _ := s.Get(ctx, "turn/0"); ok {
		t.Fatal("key should be gone after Delete")
	}
}

// TestJetStreamState_DurableAcrossInstances is the property node-local metadata
// state cannot give: a fresh State instance (i.e. a different pod) sees the run
// state a prior instance wrote.
func TestJetStreamState_DurableAcrossInstances(t *testing.T) {
	ctx := context.Background()
	kv := execKV(t)

	podA := newExecState(kv, "run-42")
	if err := podA.Set(ctx, "turn/2", []byte("cat")); err != nil {
		t.Fatalf("podA.Set: %v", err)
	}

	podB := newExecState(kv, "run-42") // different instance == different pod
	v, ok, err := podB.Get(ctx, "turn/2")
	if err != nil || !ok || string(v) != "cat" {
		t.Fatalf("podB should read podA's write: (%q,%v,%v)", v, ok, err)
	}
}

func TestJetStreamState_RunIsolation(t *testing.T) {
	ctx := context.Background()
	kv := execKV(t)
	_ = newExecState(kv, "run-a").Set(ctx, "k", []byte("A"))
	_ = newExecState(kv, "run-b").Set(ctx, "k", []byte("B"))

	if v, _, _ := newExecState(kv, "run-a").Get(ctx, "k"); string(v) != "A" {
		t.Fatalf("run-a leaked: got %q", v)
	}
	if v, _, _ := newExecState(kv, "run-b").Get(ctx, "k"); string(v) != "B" {
		t.Fatalf("run-b leaked: got %q", v)
	}
}

// memState is a minimal node-scope backend to prove ScopedRouter delegation.
type memState struct{ m map[string][]byte }

func newMem() *memState { return &memState{m: map[string][]byte{}} }
func (s *memState) Get(_ context.Context, k string) ([]byte, bool, error) {
	v, ok := s.m[k]
	return v, ok, nil
}
func (s *memState) Set(_ context.Context, k string, v []byte) error { s.m[k] = v; return nil }
func (s *memState) Delete(_ context.Context, k string) error       { delete(s.m, k); return nil }
func (s *memState) List(_ context.Context, _ string) ([]string, error) {
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	return out, nil
}
func (s *memState) Scoped(_, _ string) module.State { return s }

func TestScopedRouter_Routing(t *testing.T) {
	ctx := context.Background()
	node := newMem()
	router := &ScopedRouter{node: node, kv: execKV(t)}

	// unscoped calls hit the node backend
	if err := router.Set(ctx, "cfg", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if _, ok := node.m["cfg"]; !ok {
		t.Fatal("unscoped Set should land on the node backend")
	}

	// execution scope is durable + separate from node
	exec := router.Scoped(module.ScopeExecution, "run-x")
	if _, isJS := exec.(*JetStreamState); !isJS {
		t.Fatalf("execution scope should be JetStreamState, got %T", exec)
	}
	_ = exec.Set(ctx, "turn/0", []byte("durable"))
	if _, ok := node.m["turn/0"]; ok {
		t.Fatal("execution-scoped write must NOT land on the node backend")
	}

	// unknown scope falls back to node
	if got := router.Scoped(module.ScopeNode, "x"); got != node {
		t.Fatalf("node scope should fall back to the node backend, got %T", got)
	}
}

func TestScopedRouter_NoBrokerDegradesToNode(t *testing.T) {
	// factory with nil kv returns the bare node state; Scoped(execution) then
	// degrades to node scope rather than erroring.
	node := newMem()
	r := &ScopedRouter{node: node, kv: nil}
	if got := r.Scoped(module.ScopeExecution, "run"); got != node {
		t.Fatalf("no broker should degrade execution scope to node, got %T", got)
	}
}

func TestSanitizeKey(t *testing.T) {
	cases := map[string]string{
		"turn/0":        "turn/0",
		"a.b_c-d=e":     "a.b_c-d=e",
		"has space":     "has_space",
		"weird:*chars!": "weird__chars_",
	}
	for in, want := range cases {
		if got := sanitizeKey(in); got != want {
			t.Errorf("sanitizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}
