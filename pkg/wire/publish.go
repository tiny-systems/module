// Package wire is the public surface external callers use to inject
// work into a running flow without going through the TinySignal CRD.
// The platform's send-signal handler, the mcp-server signal_sender,
// and the desktop debugger all publish via Publish; the SDK receiver
// (internal/transport) dispatches based on the same headers the
// inter-module wire already uses.
//
// Callers obtain a *nats.Conn however they can — direct in-cluster
// for hosted, port-forwarded for BYOC clusters via the existing
// port-forwarder service.
package wire

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/tiny-systems/module/module"
	perrors "github.com/tiny-systems/module/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Header names carried on every cross-module message. Mirrors the
// constants in internal/transport — duplicated here because external
// callers can't reach internal/. Drift between the two sets would
// break dispatch silently, so any change here needs the matching
// change in internal/transport/nats.go.
const (
	HeaderFrom       = "x-from"
	HeaderTo         = "x-to"
	HeaderEdgeID     = "x-edge-id"
	HeaderReplyInbox = "x-reply-inbox"
	HeaderError      = "x-error"
	HeaderErrorCode  = "x-error-code"
	HeaderEmpty      = "x-empty"
)

const subjectPrefix = "tinymodule"

// FromSignal is the Options.From value that marks a message as an
// external signal (dashboard Send, MCP send_signal, debug). The
// runner unmarshals the raw payload into the port struct ONLY for
// signal-originated messages — any other From (including empty)
// makes it fall back to edge-config evaluation or the port's default
// configuration, silently discarding the payload. Mirrors
// runner.FromSignal, which lives in internal/ and can't be imported
// by external publishers.
const FromSignal = "signal"

// SubjectFor returns the business-hop NATS subject for a given
// module. Senders publish here for edge-driven hops; the module's
// queue-group consumer load-balances among pods.
func SubjectFor(moduleName string) string {
	return fmt.Sprintf("%s.%s.msg", subjectPrefix, moduleName)
}

// SubjectForSystem returns the fan-out subject used for system-port
// writes (_control / _settings / _reconcile / _identity). Each pod
// of the module has its own durable consumer on this subject, so
// every pod receives the message and the in-handler IsLeader check
// gates the action. Used when the target port starts with "_".
func SubjectForSystem(moduleName string) string {
	return fmt.Sprintf("%s.%s.sysmsg", subjectPrefix, moduleName)
}

// Options controls a single Publish call.
type Options struct {
	// EdgeID correlates the message back to a specific TinyNodeEdge.
	// Empty for externally-triggered signals (dashboard, MCP, debug).
	EdgeID string

	// From identifies the upstream node:port. Empty for external
	// triggers; the runtime treats empty as "no upstream."
	From string

	// WaitForReply blocks until the receiver writes back on a
	// per-call core-NATS inbox. False = fire-and-forget, returns
	// nil bytes once the broker accepts the publish.
	WaitForReply bool

	// Timeout caps WaitForReply. Zero defaults to 30s. Ignored
	// when WaitForReply is false.
	Timeout time.Duration
}

// Publish delivers a message to <targetNode>:<port>. targetNode is
// the node's full name (flowID.module.nodename); port is the port
// to deliver onto. Returns the reply payload when opts.WaitForReply
// is true; (nil, nil) on fire-and-forget success.
//
// Replaces TinySignal CR creation as the boundary primitive: same
// effect (dispatch to a specific node:port from outside the flow
// graph) without K8s API writes or controller round-trips.
func Publish(ctx context.Context, nc *nats.Conn, targetNode, port string, data []byte, opts Options) ([]byte, error) {
	if nc == nil {
		return nil, fmt.Errorf("nats conn is nil")
	}
	if strings.IndexByte(targetNode, ':') >= 0 {
		return nil, fmt.Errorf("targetNode must be the bare node FQN without :port — got %q", targetNode)
	}
	moduleName, _, err := module.ParseFullName(targetNode)
	if err != nil {
		return nil, fmt.Errorf("parse target node: %w", err)
	}

	// System-port writes fan out to every pod via the sysmsg subject;
	// business-port hops go to the queue-group subject. The single
	// "_" check captures _control, _settings, _reconcile, _identity
	// and any future system port without enumerating them.
	subject := SubjectFor(moduleName)
	if strings.HasPrefix(port, "_") {
		subject = SubjectForSystem(moduleName)
	}

	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  nats.Header{},
	}
	msg.Header.Set(HeaderTo, targetNode+":"+port)
	if opts.From != "" {
		msg.Header.Set(HeaderFrom, opts.From)
	}
	if opts.EdgeID != "" {
		msg.Header.Set(HeaderEdgeID, opts.EdgeID)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))

	if !opts.WaitForReply {
		if err := nc.PublishMsg(msg); err != nil {
			return nil, fmt.Errorf("publish: %w", err)
		}
		return nil, nil
	}

	inbox := nats.NewInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return nil, fmt.Errorf("subscribe reply inbox: %w", err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	msg.Header.Set(HeaderReplyInbox, inbox)
	// The receive side (transport.handleIncoming) replies with
	// m.RespondMsg, which publishes to the NATS-native Reply field —
	// it never reads the x-reply-inbox header. Without msg.Reply set
	// here every WaitForReply call timed out even on successful
	// delivery. Keep the header for observability, but the native
	// Reply field is what makes the round-trip work.
	msg.Reply = inbox

	if err := nc.PublishMsg(msg); err != nil {
		return nil, fmt.Errorf("publish: %w", err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	reply, err := sub.NextMsgWithContext(waitCtx)
	if err != nil {
		return nil, err
	}
	if e := reply.Header.Get(HeaderError); e != "" {
		if code := reply.Header.Get(HeaderErrorCode); code != "" {
			return nil, perrors.NonRetryable(code, errors.New(e))
		}
		return nil, errors.New(e)
	}
	if reply.Header.Get(HeaderEmpty) == "1" || len(reply.Data) == 0 {
		return nil, nil
	}
	return reply.Data, nil
}
