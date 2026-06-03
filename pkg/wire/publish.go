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

// SubjectFor returns the NATS subject a given module subscribes to.
// Senders publish here; the SDK pod for `moduleName` consumes.
func SubjectFor(moduleName string) string {
	return fmt.Sprintf("%s.%s.msg", subjectPrefix, moduleName)
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

	msg := &nats.Msg{
		Subject: SubjectFor(moduleName),
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
