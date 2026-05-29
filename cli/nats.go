package cli

import (
	"context"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// connectJetStream returns a JetStream handle when TINY_NATS_URL is set
// in the env, nil otherwise. Failure to connect logs a warning and
// returns nil — modules that opted into NATSAware fall back to legacy
// behavior, so a broker outage at boot doesn't crash the module pod.
//
// The connection is intentionally not closed here; it lives for the
// lifetime of the module process. Module shutdown closes the nats
// connection via the runtime's signal handling indirectly (process
// exit drains TCP).
func connectJetStream(ctx context.Context, l logr.Logger) jetstream.JetStream {
	url := os.Getenv("TINY_NATS_URL")
	if url == "" {
		return nil
	}

	nc, err := nats.Connect(
		url,
		nats.Name("tiny-module"),
		// 5s is generous for an in-cluster service. Modules shouldn't
		// hang the lifecycle longer than that waiting on a broker
		// that may not exist yet — fall through to nil and let
		// NATSAware components degrade.
		nats.Timeout(5*time.Second),
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		l.Info("TINY_NATS_URL set but nats.Connect failed; NATSAware components will receive nil",
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
		nc.Close()
		return nil
	}

	l.Info("NATS JetStream connected", "url", url)
	return js
}
