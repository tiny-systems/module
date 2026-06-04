// RPC dispatch helper — agent MCP tool calls go through this. Unlike
// Publish (which targets a node:port inside a flow), PublishRPC fires
// at a bare component name on a module and waits for the reply on a
// per-call inbox. The SDK receiver short-circuits the node dispatch
// when it sees x-mode: rpc and runs the component fresh.

package wire

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	perrors "github.com/tiny-systems/module/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// headerMode mirrors internal/transport's constant. Duplicated here
// because external callers can't reach internal/. Any change to the
// header name needs to update both.
const headerMode = "x-mode"

// RPCOptions controls a single PublishRPC call. Mirrors Options but
// drops fields that don't apply (EdgeID, From, WaitForReply — RPC is
// always a request/reply round-trip).
type RPCOptions struct {
	// Timeout caps how long to wait for the receiver's reply. Zero
	// defaults to 30s. Long-running tools (LLM completions, image
	// generation) should set this explicitly.
	Timeout time.Duration
}

// PublishRPC invokes <componentName> on <moduleName> as an MCP-style
// tool call. data is the JSON payload the component's input port
// expects; the returned bytes are the JSON output the component
// emitted on its configured AgentTool output port.
//
// Returns an error if the receiver couldn't find / instantiate the
// component, the component returned Fail, or the reply didn't arrive
// before Timeout.
func PublishRPC(ctx context.Context, nc *nats.Conn, moduleName, componentName string, data []byte, opts RPCOptions) ([]byte, error) {
	if nc == nil {
		return nil, fmt.Errorf("nats conn is nil")
	}
	if moduleName == "" {
		return nil, fmt.Errorf("moduleName is required")
	}
	if componentName == "" {
		return nil, fmt.Errorf("componentName is required")
	}

	inbox := nats.NewInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return nil, fmt.Errorf("subscribe reply inbox: %w", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	msg := &nats.Msg{
		Subject: SubjectFor(moduleName),
		Data:    data,
		Header:  nats.Header{},
	}
	msg.Header.Set(HeaderTo, componentName)
	msg.Header.Set(headerMode, "rpc")
	msg.Header.Set(HeaderReplyInbox, inbox)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))

	if err := nc.PublishMsg(msg); err != nil {
		return nil, fmt.Errorf("publish rpc: %w", err)
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
