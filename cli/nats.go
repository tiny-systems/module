package cli

import (
	"context"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// natsRuntime bundles the live NATS connection + JetStream context.
// Both are needed: nc backs the cross-module transport (core
// request/reply), js backs the optional NATSAware capability. The
// runtime owns the connection for the lifetime of the process.
type natsRuntime struct {
	NC *nats.Conn
	JS jetstream.JetStream
}

// connectNATS returns a populated runtime when TINY_NATS_URL is set,
// nil otherwise. Failure to connect logs a warning and returns nil —
// the SDK falls back to legacy gRPC transport in that case so a
// broker outage at boot doesn't crash the module pod.
//
// The connection is not closed here; it lives until the module
// process exits.
func connectNATS(ctx context.Context, l logr.Logger) *natsRuntime {
	url := os.Getenv("TINY_NATS_URL")
	if url == "" {
		return nil
	}

	nc, err := nats.Connect(
		url,
		nats.Name("tiny-module"),
		// 5s connect timeout — modules shouldn't block startup on a
		// broker that may not be up yet. If connect fails the runtime
		// falls through to gRPC.
		nats.Timeout(5*time.Second),
		// Indefinite reconnects with a 2s backoff. The active session
		// is best-effort but the long-lived subscription should
		// recover automatically when the broker comes back.
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		l.Info("TINY_NATS_URL set but nats.Connect failed; falling back to gRPC transport",
			"url", url,
			"err", err.Error(),
		)
		return nil
	}

	js, err := jetstream.New(nc)
	if err != nil {
		l.Info("nats connected but jetstream.New failed; NATSAware components will receive nil",
			"err", err.Error(),
		)
		// nc is still usable for the core request/reply transport, so
		// don't close it — just leave js nil.
		return &natsRuntime{NC: nc, JS: nil}
	}

	l.Info("NATS connected", "url", url)
	return &natsRuntime{NC: nc, JS: js}
}

// connectJetStream is the legacy entry point used by older call sites
// that only needed the JetStream handle. Forwards to connectNATS so
// the connection lifecycle stays single-sourced.
//
// Deprecated: prefer connectNATS — it returns both nc and js.
func connectJetStream(ctx context.Context, l logr.Logger) jetstream.JetStream {
	rt := connectNATS(ctx, l)
	if rt == nil {
		return nil
	}
	return rt.JS
}
