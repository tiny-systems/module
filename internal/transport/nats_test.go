package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	perrors "github.com/tiny-systems/module/pkg/errors"
)

// startNATS boots an embedded nats-server on a random port and returns
// (url, shutdown). JetStream stays off — this test file covers the
// core req/reply path; JS-backed tests will live in their own file
// when that path lands.
func startNATS(t *testing.T) (string, func()) {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = -1
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(2 * time.Second) {
		t.Fatal("nats server not ready")
	}
	return srv.ClientURL(), func() { srv.Shutdown() }
}

// newTransport pairs a NATS connection with a transport.NATS bound to
// the named module. The connection is closed on test cleanup.
func newTransport(t *testing.T, url, moduleName string) (*nats.Conn, *NATS) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc, NewNATS(nc, moduleName, logr.Discard())
}

// runReceiver starts the receiver in a goroutine bound to the test
// lifetime. Returns the running ctx so tests can assert handler-side
// behavior via the supplied handler closure.
func runReceiver(t *testing.T, tr *NATS, h runner.Handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = tr.StartReceiver(ctx, h) }()
	// Give the subscription a tick to register before tests fire.
	time.Sleep(50 * time.Millisecond)
}

// silencedNATSLog routes nats-server's own chatter to /dev/null so
// the test output stays readable.
var _ = silencedNATSLog()

func silencedNATSLog() server.Logger { return nil }

// ---------------------------------------------------------------------
// Round-trip — sender publishes, receiver handles, sender gets reply.
// ---------------------------------------------------------------------

func TestRoundTrip(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, receiver := newTransport(t, url, "target")

	runReceiver(t, receiver, func(ctx context.Context, msg *runner.Msg) (any, error) {
		return []byte(`{"ok":true}`), nil
	})

	resp, err := sender.Handler(context.Background(), &runner.Msg{
		To:     "flow.target.node",
		From:   "flow.sender.node",
		EdgeID: "edge-1",
		Data:   []byte("hello"),
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if string(resp) != `{"ok":true}` {
		t.Fatalf("unexpected resp: %q", string(resp))
	}
}

// ---------------------------------------------------------------------
// Empty reply — receiver returns nil, sender's Handler returns nil
// without trying to unmarshal a null-byte payload.
// ---------------------------------------------------------------------

func TestEmptyReplyReturnsNil(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, receiver := newTransport(t, url, "target")

	runReceiver(t, receiver, func(ctx context.Context, msg *runner.Msg) (any, error) {
		return nil, nil
	})

	resp, err := sender.Handler(context.Background(), &runner.Msg{
		To:     "flow.target.node",
		EdgeID: "edge-empty",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil resp on empty reply, got %v (len=%d)", resp, len(resp))
	}
}

// ---------------------------------------------------------------------
// Error propagation — handler returns error, sender surfaces it.
// ---------------------------------------------------------------------

func TestErrorPropagation(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, receiver := newTransport(t, url, "target")

	runReceiver(t, receiver, func(ctx context.Context, msg *runner.Msg) (any, error) {
		return nil, errors.New("kaboom")
	})

	_, err := sender.Handler(context.Background(), &runner.Msg{
		To:     "flow.target.node",
		EdgeID: "edge-err",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "kaboom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------
// Header propagation — receiver sees From / To / EdgeID / Depth from
// the original runner.Msg.
// ---------------------------------------------------------------------

func TestHeaderPropagation(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, receiver := newTransport(t, url, "target")

	var seen *runner.Msg
	runReceiver(t, receiver, func(ctx context.Context, msg *runner.Msg) (any, error) {
		seen = msg
		return nil, nil
	})

	_, err := sender.Handler(context.Background(), &runner.Msg{
		To:     "flow.target.node",
		From:   "flow.sender.source",
		EdgeID: "edge-headers",
		Depth:  7,
		Data:   []byte(`{"k":"v"}`),
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if seen == nil {
		t.Fatal("receiver did not record msg")
	}
	if seen.EdgeID != "edge-headers" {
		t.Errorf("EdgeID: %q", seen.EdgeID)
	}
	if seen.From != "flow.sender.source" {
		t.Errorf("From: %q", seen.From)
	}
	if seen.To != "flow.target.node" {
		t.Errorf("To: %q", seen.To)
	}
	if seen.Depth != 7 {
		t.Errorf("Depth: %d", seen.Depth)
	}
	if string(seen.Data) != `{"k":"v"}` {
		t.Errorf("Data: %q", string(seen.Data))
	}
}

// ---------------------------------------------------------------------
// Context cancellation — sender's blocked Handler unblocks on cancel.
// ---------------------------------------------------------------------

func TestContextCancellation(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, receiver := newTransport(t, url, "target")

	hold := make(chan struct{})
	runReceiver(t, receiver, func(ctx context.Context, msg *runner.Msg) (any, error) {
		<-hold
		return nil, nil
	})
	t.Cleanup(func() { close(hold) })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := sender.Handler(ctx, &runner.Msg{To: "flow.target.node", EdgeID: "edge-cancel"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error on cancel")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Handler didn't unblock promptly on cancel: %v", elapsed)
	}
}

// ---------------------------------------------------------------------
// Concurrent in-flight — N senders fire simultaneously; the receiver's
// goroutine-per-message dispatch must not serialize them.
// ---------------------------------------------------------------------

func TestConcurrentInFlight(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, receiver := newTransport(t, url, "target")

	const N = 20
	const handlerDelay = 200 * time.Millisecond

	var concurrent int32
	var peak int32
	runReceiver(t, receiver, func(ctx context.Context, msg *runner.Msg) (any, error) {
		cur := atomic.AddInt32(&concurrent, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(handlerDelay)
		atomic.AddInt32(&concurrent, -1)
		return []byte("ok"), nil
	})

	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := sender.Handler(context.Background(), &runner.Msg{
				To:     "flow.target.node",
				EdgeID: fmt.Sprintf("edge-%d", i),
			})
			if err != nil {
				t.Errorf("Handler[%d]: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Sequential processing would take N * handlerDelay = 4s for N=20.
	// Goroutine-per-message should be well under that — the bar here
	// is "obviously concurrent", not "perfectly parallel".
	if elapsed > time.Duration(N)*handlerDelay/4 {
		t.Errorf("processing looked sequential: %v for %d msgs (handler %v)", elapsed, N, handlerDelay)
	}
	if peak < 2 {
		t.Errorf("peak concurrency was %d — handler ran sequentially", peak)
	}
}

// ---------------------------------------------------------------------
// Nested request — receiver inside its handler issues a Handler call
// back to the original sender. With the goroutine-per-message fix
// (v0.10.24) this completes; without it, the second message queues
// behind the first's pending reply and both time out.
// ---------------------------------------------------------------------

func TestNestedRequestNoDeadlock(t *testing.T) {
	url, _ := startNATS(t)
	_, modA := newTransport(t, url, "module-a")
	_, modB := newTransport(t, url, "module-b")

	runReceiver(t, modA, func(ctx context.Context, msg *runner.Msg) (any, error) {
		return []byte("from-A"), nil
	})
	runReceiver(t, modB, func(ctx context.Context, msg *runner.Msg) (any, error) {
		// B re-enters A while still holding the first message's reply.
		resp, err := modB.Handler(ctx, &runner.Msg{
			To:     "flow.module-a.node",
			EdgeID: "nested-" + msg.EdgeID,
		})
		if err != nil {
			return nil, err
		}
		return resp, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := modA.Handler(ctx, &runner.Msg{
		To:     "flow.module-b.node",
		EdgeID: "outer",
	})
	if err != nil {
		t.Fatalf("nested call failed: %v", err)
	}
	if string(resp) != "from-A" {
		t.Fatalf("unexpected resp: %q", string(resp))
	}
}

// ---------------------------------------------------------------------
// Queue group — two receivers in the same module name share the
// workload; one message goes to exactly one of them.
// ---------------------------------------------------------------------

func TestQueueGroupLoadBalancing(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, r1 := newTransport(t, url, "target")
	_, r2 := newTransport(t, url, "target")

	var got1, got2 int32
	runReceiver(t, r1, func(ctx context.Context, msg *runner.Msg) (any, error) {
		atomic.AddInt32(&got1, 1)
		return []byte("r1"), nil
	})
	runReceiver(t, r2, func(ctx context.Context, msg *runner.Msg) (any, error) {
		atomic.AddInt32(&got2, 1)
		return []byte("r2"), nil
	})

	const N = 30
	for i := 0; i < N; i++ {
		if _, err := sender.Handler(context.Background(), &runner.Msg{
			To:     "flow.target.node",
			EdgeID: fmt.Sprintf("qg-%d", i),
		}); err != nil {
			t.Fatalf("Handler[%d]: %v", i, err)
		}
	}

	total := atomic.LoadInt32(&got1) + atomic.LoadInt32(&got2)
	if total != N {
		t.Fatalf("total deliveries %d, want %d (r1=%d r2=%d)", total, N, got1, got2)
	}
	// Both should have received SOMETHING — exact split varies, but a
	// queue group that funnels 100% to one consumer is a bug.
	if got1 == 0 || got2 == 0 {
		t.Errorf("queue group did not load-balance: r1=%d r2=%d", got1, got2)
	}
}

// ---------------------------------------------------------------------
// Guardrail — the transport must never silently retry on a handler
// error. Single send, single failure, no second invocation.
// (Memory feedback_no_implicit_retries.md — single-shot default.)
// ---------------------------------------------------------------------

// ---------------------------------------------------------------------
// Error code survives the wire — handler returns NonRetryable(code,
// err), sender extracts the code via perrors.ErrorCode.
// ---------------------------------------------------------------------

func TestErrorCodePropagation(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, receiver := newTransport(t, url, "target")

	runReceiver(t, receiver, func(ctx context.Context, msg *runner.Msg) (any, error) {
		return nil, perrors.NonRetryable("quota_exceeded", errors.New("tokens"))
	})

	_, err := sender.Handler(context.Background(), &runner.Msg{
		To:     "flow.target.node",
		EdgeID: "edge-coded",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if code := perrors.ErrorCode(err); code != "quota_exceeded" {
		t.Fatalf("ErrorCode = %q, want quota_exceeded", code)
	}
}

func TestNoImplicitRetryOnHandlerError(t *testing.T) {
	url, _ := startNATS(t)
	_, sender := newTransport(t, url, "sender")
	_, receiver := newTransport(t, url, "target")

	var calls int32
	runReceiver(t, receiver, func(ctx context.Context, msg *runner.Msg) (any, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("permanent")
	})

	_, err := sender.Handler(context.Background(), &runner.Msg{
		To:     "flow.target.node",
		EdgeID: "no-retry",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// Give any rogue retry a chance to fire before asserting.
	time.Sleep(200 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("handler called %d times, want exactly 1", n)
	}
}
