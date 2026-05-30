// Package transport carries the cross-module wire. Today supports two
// substrates: gRPC (existing AddressPool in internal/client) and NATS
// core request/reply (this file). cli/run.go selects one or the other
// based on TINY_NATS_URL — set = NATS, unset = gRPC.
//
// Same runner.Handler contract for both: the scheduler doesn't know
// (or care) which wire carries an edge. The blocking I/O model is
// preserved on NATS via core request/reply — caller blocks on
// nats.RequestMsg until the responder writes back on the reply inbox
// or the ctx deadline fires.
//
// Subject layout: `tinymodule.<module-name>.msg`. Each module pod
// subscribes once at startup with a queue group equal to its own
// module name so multiple replicas of the same module load-balance
// incoming messages (mirroring K8s Service's round-robin gRPC today).
//
// Wire metadata travels in NATS headers:
//   x-from           — source FullName
//   x-to             — target FullName
//   x-edge-id        — EdgeID, also stamped as Nats-Msg-Id for dedup
//   x-message-depth  — Depth, for cycle detection (MaxMessageDepth)
//   x-error          — on the reply only; non-empty = handler failure
//   traceparent      — W3C trace context for OTel propagation
package transport

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/nats-io/nats.go"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	module2 "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	subjectPrefix      = "tinymodule"
	headerFrom         = "x-from"
	headerTo           = "x-to"
	headerEdgeID       = "x-edge-id"
	headerMessageDepth = "x-message-depth"
	headerError        = "x-error"
	natsMsgIDHeader    = "Nats-Msg-Id"
)

// SubjectFor returns the NATS subject a given module subscribes to and
// senders publish to. Subject = `tinymodule.<module-name>.msg`.
func SubjectFor(moduleName string) string {
	return fmt.Sprintf("%s.%s.msg", subjectPrefix, moduleName)
}

// NATS holds the cross-module wire backed by NATS core request/reply.
// Implements both sender (Handler) and receiver (StartReceiver) so a
// single instance replaces both AddressPool and the gRPC server.
type NATS struct {
	nc         *nats.Conn
	moduleName string
	log        logr.Logger
	// requestTimeout caps how long the sender blocks on a reply when
	// the caller's ctx has no deadline. 5m matches typical LLM tool
	// call upper bound; flows with longer-running components must
	// pass an explicit ctx deadline.
	requestTimeout time.Duration
}

// NewNATS builds the transport. moduleName is the local module's name
// (matches --name ldflags); the receiver subscribes to its subject.
func NewNATS(nc *nats.Conn, moduleName string, log logr.Logger) *NATS {
	return &NATS{
		nc:             nc,
		moduleName:     moduleName,
		log:            log,
		requestTimeout: 5 * time.Minute,
	}
}

// Handler is the sender side. Matches the signature scheduler expects
// for cross-module delivery (same shape as AddressPool.Handler).
//
// Blocks until: (a) responder replies on the inbox, (b) ctx is
// cancelled, or (c) requestTimeout elapses with no deadline on ctx.
func (t *NATS) Handler(ctx context.Context, msg *runner.Msg) ([]byte, error) {
	moduleName, _, err := module2.ParseFullName(msg.To)
	if err != nil {
		return nil, err
	}

	natsMsg := &nats.Msg{
		Subject: SubjectFor(moduleName),
		Data:    msg.Data,
		Header:  nats.Header{},
	}
	natsMsg.Header.Set(headerFrom, msg.From)
	natsMsg.Header.Set(headerTo, msg.To)
	natsMsg.Header.Set(headerEdgeID, msg.EdgeID)
	natsMsg.Header.Set(natsMsgIDHeader, msg.EdgeID)
	if msg.Depth > 0 {
		natsMsg.Header.Set(headerMessageDepth, strconv.Itoa(msg.Depth))
	}

	// Inject W3C trace context so the responder's span can attach to
	// the caller's. propagation.HeaderCarrier wraps a map-like the
	// nats.Header satisfies.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(natsMsg.Header))

	// If ctx has a deadline, use it; otherwise fall back to the
	// transport's default. NATS RequestMsgWithContext respects ctx
	// cancellation either way.
	reqCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, t.requestTimeout)
		defer cancel()
	}

	reply, err := t.nc.RequestMsgWithContext(reqCtx, natsMsg)
	if err != nil {
		return nil, fmt.Errorf("nats request to %s: %w", moduleName, err)
	}

	// Handler-side error: surface back to caller as a Go error so the
	// scheduler's Result chain treats it like a gRPC error today.
	if e := reply.Header.Get(headerError); e != "" {
		return nil, errors.New(e)
	}

	return reply.Data, nil
}

// StartReceiver subscribes to this module's subject and dispatches
// each incoming message to the scheduler via handler. Uses a queue
// group equal to the module name so a Deployment with N replicas
// gets round-robin delivery, mirroring the K8s Service load-balance
// today.
//
// Blocks until ctx is cancelled, then drains and unsubscribes.
func (t *NATS) StartReceiver(ctx context.Context, handler runner.Handler) error {
	subject := SubjectFor(t.moduleName)
	queue := t.moduleName

	// Each incoming message is dispatched in its own goroutine. The
	// NATS Go client's callback runs in a single goroutine per
	// subscription by default — sequential processing would deadlock
	// the moment a handler issues a downstream nats.Request whose
	// target is another node on the same module (the second message
	// queues behind the first one's still-pending reply).
	sub, err := t.nc.QueueSubscribe(subject, queue, func(m *nats.Msg) {
		go t.handleIncoming(ctx, handler, m)
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}

	t.log.Info("nats transport: receiver subscribed",
		"subject", subject,
		"queue", queue,
	)

	<-ctx.Done()

	// Drain in flight messages, then unsubscribe. Drain returns after
	// the subscription stops delivering — caller's ctx cancel will
	// already have aborted any in-flight handler calls.
	if drainErr := sub.Drain(); drainErr != nil {
		t.log.Info("nats transport: drain on shutdown",
			"err", drainErr.Error(),
		)
	}
	return nil
}

func (t *NATS) handleIncoming(parentCtx context.Context, handler runner.Handler, m *nats.Msg) {
	// Extract trace context from headers BEFORE we touch any other
	// header — keeps the span hierarchy intact for OTel exporters.
	carrier := propagation.HeaderCarrier(m.Header)
	ctx := otel.GetTextMapPropagator().Extract(parentCtx, carrier)

	depth := 0
	if d := m.Header.Get(headerMessageDepth); d != "" {
		depth, _ = strconv.Atoi(d)
	}

	res, err := handler(ctx, &runner.Msg{
		EdgeID: m.Header.Get(headerEdgeID),
		To:     m.Header.Get(headerTo),
		From:   m.Header.Get(headerFrom),
		Data:   m.Data,
		Depth:  depth,
	})
	if err != nil {
		// Reply with an error header. Caller's Handler() reads
		// x-error and surfaces it as a Go error to the runner.
		reply := &nats.Msg{
			Header: nats.Header{headerError: []string{err.Error()}},
		}
		if respErr := m.RespondMsg(reply); respErr != nil {
			t.log.Info("nats transport: respond on error failed",
				"err", respErr.Error(),
			)
		}
		return
	}

	var payload []byte
	switch {
	case utils.IsNil(res):
		payload = nil
	case isBytes(res):
		payload = res.([]byte)
	default:
		payload, err = json.Marshal(res)
		if err != nil {
			t.log.Info("nats transport: marshal response failed",
				"err", err.Error(),
			)
			payload = nil
		}
	}

	if respErr := m.Respond(payload); respErr != nil {
		t.log.Info("nats transport: respond failed",
			"err", respErr.Error(),
		)
	}
}

func isBytes(v interface{}) bool {
	_, ok := v.([]byte)
	return ok
}
