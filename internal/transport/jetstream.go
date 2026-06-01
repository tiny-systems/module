// JetStream-backed cross-module wire — same runner.Handler shape as
// the core-NATS transport in nats.go, with durability layered in:
//
//   - Requests publish to a JetStream stream (`module-edges`) so they
//     survive a sender pod restart between publish and reply.
//   - Receivers run as durable consumers per module. AckWait is the
//     pod-death detector — if a consumer pod dies mid-handler, the
//     message is redelivered to another consumer after the wait
//     expires. `InProgress()` extensions keep AckWait from firing
//     under slow LLM calls.
//   - Replies still travel on a core-NATS inbox subscription (no
//     durability needed — the caller is the only one waiting). If the
//     caller's pod died, the reply lands on a dead inbox and is
//     dropped — same as today.
//
// Single-shot on handler error: when the handler returns an error,
// the message is `Term`-ed, never redelivered. Only pod-death
// (AckWait expiry) triggers redelivery. The runtime never silently
// retries logical failures (matches feedback_no_implicit_retries.md
// — per-edge retry policy is the explicit opt-in, layered on top).
package transport

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	module2 "github.com/tiny-systems/module/module"
	perrors "github.com/tiny-systems/module/pkg/errors"
	"github.com/tiny-systems/module/pkg/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// EdgeStreamName is the JetStream stream holding cross-module
	// requests. WorkQueue retention so messages disappear after Ack —
	// the stream isn't an audit log, it's an in-flight buffer.
	EdgeStreamName = "module-edges"

	// EdgeStreamSubjects is the subject filter the stream binds to.
	// Each module's consumer filters to its own `tinymodule.<name>.msg`.
	EdgeStreamSubjects = "tinymodule.*.msg"

	// edgeAckWait is the broker-side deadline before a delivered
	// message is considered abandoned and redelivered. Sized for the
	// 5-minute requestTimeout used by the sender; long-running
	// handlers keep AckWait alive via InProgress extensions below.
	edgeAckWait = 5 * time.Minute

	// edgeInProgressInterval ticks well below AckWait so we never
	// race the broker. Off by ~30 % gives plenty of slack on a
	// loaded network.
	edgeInProgressInterval = 90 * time.Second

	// edgeMaxDeliver caps broker-driven redelivery on AckWait expiry.
	// Three pod-death recoveries is generous — beyond that the
	// message is likely poison. Handler-error Term() doesn't count
	// against this; it ends delivery immediately.
	edgeMaxDeliver = 3
)

// EnsureEdgeStream creates or updates the module-edges stream. Called
// once at SDK startup before any receiver subscribes; safe to call
// from every module pod concurrently — JetStream resolves the
// CreateOrUpdate idempotently.
func EnsureEdgeStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       EdgeStreamName,
		Subjects:   []string{EdgeStreamSubjects},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.WorkQueuePolicy,
		MaxAge:     1 * time.Hour,
		Duplicates: 2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("ensure edge stream: %w", err)
	}
	return nil
}

// JetStream is the durable-wire counterpart of NATS (in nats.go).
// Same Handler / StartReceiver surface; the scheduler can swap
// substrates without touching component code.
type JetStream struct {
	js             jetstream.JetStream
	nc             *nats.Conn // reply inbox subscription lives on core NATS
	moduleName     string
	log            logr.Logger
	requestTimeout time.Duration
}

// NewJetStream pairs a NATS connection with its JetStream context.
// `nc` is the same connection `js` was built from — kept on the
// struct because the reply inbox subscription is a core-NATS op.
func NewJetStream(js jetstream.JetStream, nc *nats.Conn, moduleName string, log logr.Logger) *JetStream {
	return &JetStream{
		js:             js,
		nc:             nc,
		moduleName:     moduleName,
		log:            log,
		requestTimeout: 5 * time.Minute,
	}
}

// Handler — sender side. Publishes the request to the durable
// stream, blocks on the reply inbox until the receiver writes back
// or the caller's ctx fires.
func (t *JetStream) Handler(ctx context.Context, msg *runner.Msg) ([]byte, error) {
	moduleName, _, err := module2.ParseFullName(msg.To)
	if err != nil {
		return nil, err
	}

	// Per-request reply inbox. The receiver writes the reply via core
	// NATS (no JS persistence needed — the caller is the only reader).
	inbox := nats.NewInbox()
	replyCh := make(chan *nats.Msg, 1)
	sub, err := t.nc.ChanSubscribe(inbox, replyCh)
	if err != nil {
		return nil, fmt.Errorf("jetstream subscribe inbox: %w", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	natsMsg := &nats.Msg{
		Subject: SubjectFor(moduleName),
		Data:    msg.Data,
		Header:  nats.Header{},
	}
	// JetStream's m.Reply() on the receiver is the broker's internal
	// ack subject — NOT the original publisher's reply. Carry the
	// inbox explicitly in a header so the receiver routes its core-
	// NATS reply to the right place.
	natsMsg.Header.Set(headerReplyInbox, inbox)
	natsMsg.Header.Set(headerFrom, msg.From)
	natsMsg.Header.Set(headerTo, msg.To)
	natsMsg.Header.Set(headerEdgeID, msg.EdgeID)
	// Intentionally not stamping Nats-Msg-Id with EdgeID here. JS dedup
	// silently drops the 2nd publish from the stream — the sender then
	// blocks forever on its reply inbox. Once per-edge retry policy
	// lands with attempt-aware identifiers (EdgeID + attempt #), the
	// dedup key can come back to guard genuine duplicates without
	// killing legitimate retries.
	if msg.Depth > 0 {
		natsMsg.Header.Set(headerMessageDepth, strconv.Itoa(msg.Depth))
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(natsMsg.Header))

	// Publish to the durable stream. Returning before the broker
	// acknowledges the publish would lose durability — wait for the
	// PublishMsg ack synchronously.
	if _, err := t.js.PublishMsg(ctx, natsMsg); err != nil {
		return nil, fmt.Errorf("jetstream publish to %s: %w", moduleName, err)
	}

	reqCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, t.requestTimeout)
		defer cancel()
	}

	select {
	case reply := <-replyCh:
		if e := reply.Header.Get(headerError); e != "" {
			if code := reply.Header.Get(headerErrorCode); code != "" {
				return nil, perrors.NonRetryable(code, errors.New(e))
			}
			return nil, errors.New(e)
		}
		if reply.Header.Get(headerEmpty) == "1" || len(reply.Data) == 0 {
			return nil, nil
		}
		return reply.Data, nil
	case <-reqCtx.Done():
		return nil, reqCtx.Err()
	}
}

// StartReceiver creates a durable consumer for this module and
// dispatches each delivery to handler in its own goroutine.
func (t *JetStream) StartReceiver(ctx context.Context, handler runner.Handler) error {
	subject := SubjectFor(t.moduleName)
	consumer, err := t.js.CreateOrUpdateConsumer(ctx, EdgeStreamName, jetstream.ConsumerConfig{
		Durable:       t.moduleName,
		FilterSubject: subject,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       edgeAckWait,
		MaxDeliver:    edgeMaxDeliver,
		ReplayPolicy:  jetstream.ReplayInstantPolicy,
	})
	if err != nil {
		return fmt.Errorf("jetstream consumer for %s: %w", subject, err)
	}

	cc, err := consumer.Consume(func(m jetstream.Msg) {
		go t.handleIncoming(ctx, handler, m)
	})
	if err != nil {
		return fmt.Errorf("jetstream consume on %s: %w", subject, err)
	}

	t.log.Info("jetstream transport: receiver subscribed",
		"subject", subject,
		"consumer", t.moduleName,
	)

	<-ctx.Done()
	cc.Stop()
	return nil
}

func (t *JetStream) handleIncoming(parentCtx context.Context, handler runner.Handler, m jetstream.Msg) {
	headers := m.Headers()
	carrier := propagation.HeaderCarrier(headers)
	ctx := otel.GetTextMapPropagator().Extract(parentCtx, carrier)

	depth := 0
	if d := headers.Get(headerMessageDepth); d != "" {
		depth, _ = strconv.Atoi(d)
	}

	// InProgress ticker — keeps AckWait alive while the handler is
	// still running. Stops the moment we have a final outcome.
	stopIP := make(chan struct{})
	var ipWg sync.WaitGroup
	ipWg.Add(1)
	go func() {
		defer ipWg.Done()
		ticker := time.NewTicker(edgeInProgressInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopIP:
				return
			case <-ticker.C:
				_ = m.InProgress()
			}
		}
	}()

	res, handlerErr := handler(ctx, &runner.Msg{
		EdgeID: headers.Get(headerEdgeID),
		To:     headers.Get(headerTo),
		From:   headers.Get(headerFrom),
		Data:   m.Data(),
		Depth:  depth,
	})

	close(stopIP)
	ipWg.Wait()

	replySubject := headers.Get(headerReplyInbox)

	if handlerErr != nil {
		// Surface the error to the caller via core NATS reply, then
		// Term — single-shot on handler error, never redelivered.
		if replySubject != "" {
			reply := &nats.Msg{
				Subject: replySubject,
				Header:  nats.Header{headerError: []string{handlerErr.Error()}},
			}
			if code := perrors.ErrorCode(handlerErr); code != "" {
				reply.Header.Set(headerErrorCode, code)
			}
			if pubErr := t.nc.PublishMsg(reply); pubErr != nil {
				t.log.Info("jetstream transport: publish error reply failed",
					"err", pubErr.Error(),
				)
			}
		}
		if termErr := m.Term(); termErr != nil {
			t.log.Info("jetstream transport: term failed",
				"err", termErr.Error(),
			)
		}
		return
	}

	// Success — encode the payload, publish to the inbox, ack the
	// stream message so JS drops it from the work queue.
	var payload []byte
	switch {
	case utils.IsNil(res):
		if replySubject != "" {
			reply := &nats.Msg{
				Subject: replySubject,
				Header:  nats.Header{headerEmpty: []string{"1"}},
			}
			if pubErr := t.nc.PublishMsg(reply); pubErr != nil {
				t.log.Info("jetstream transport: publish empty reply failed",
					"err", pubErr.Error(),
				)
			}
		}
		_ = m.Ack()
		return
	case isBytes(res):
		payload = res.([]byte)
	default:
		var marshalErr error
		payload, marshalErr = json.Marshal(res)
		if marshalErr != nil {
			t.log.Info("jetstream transport: marshal response failed",
				"err", marshalErr.Error(),
			)
			payload = nil
		}
	}

	if replySubject != "" {
		if pubErr := t.nc.Publish(replySubject, payload); pubErr != nil {
			t.log.Info("jetstream transport: publish reply failed",
				"err", pubErr.Error(),
			)
		}
	}
	if ackErr := m.Ack(); ackErr != nil {
		t.log.Info("jetstream transport: ack failed",
			"err", ackErr.Error(),
		)
	}
}
