package metrics

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"os"
	"strings"
	"sync/atomic"
)

type Metric string

const (
	MetricPortMsgIn  Metric = "tiny_node_msg_in"
	MetricPortMsgOut        = "tiny_node_msg_out"
	MetricEdgeMsgIn         = "tiny_edge_msg_in"
	MetricEdgeMsgOut        = "tiny_edge_msg_out"
	MetricEdgeBusy          = "tiny_edge_busy"
)

// ConfigureOpenTelemetry configures OpenTelemetry.
// You can use OTLP_DISABLED env var to completely skip open telemetry configuration.
func ConfigureOpenTelemetry(opts ...Option) error {
	if _, ok := os.LookupEnv("OTLP_DISABLED"); ok {
		return fmt.Errorf("telemetry disabled")
	}

	ctx := context.TODO()
	conf := newConfig(opts)

	if !conf.tracingEnabled && !conf.metricsEnabled {
		return nil
	}

	dsn, err := ParseDSN(conf.dsn)
	if err != nil {
		return fmt.Errorf("invalid OTLP_DSN: %s (Opentelemetry is disabled)", err)
	}

	if dsn.Token == "<token>" {
		return fmt.Errorf("dummy OTLP_DSN detected: %q (Opentelemetry is disabled)", conf.dsn)
	}

	if strings.HasSuffix(dsn.Host, ":14318") {
		return fmt.Errorf("open-telemetry-go uses OTLP/gRPC exporter, but got host %q", dsn.Host)
	}

	c := newClient(dsn)

	configurePropagator(conf)
	if conf.tracingEnabled {
		if err = configureTracing(ctx, c, conf); err != nil {
			return err
		}
	}
	if conf.metricsEnabled {
		if err = configureMetrics(ctx, c, conf); err != nil {
			return err
		}
	}

	atomicClient.Store(c)
	return nil
}

func configurePropagator(conf *config) {
	conf.traceSampler = sdktrace.AlwaysSample()

	textMapPropagator := conf.textMapPropagator
	if textMapPropagator == nil {
		textMapPropagator = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}
	otel.SetTextMapPropagator(textMapPropagator)
}

//------------------------------------------------------------------------------

var (
	fallbackClient = newClient(&DSN{
		Scheme:   "https",
		Host:     "grpc.tinysystems.io",
		GRPCPort: "413",
		Token:    "<token>",
	})
	atomicClient atomic.Value
)

func activeClient() *client {
	v := atomicClient.Load()
	if v == nil {
		return fallbackClient
	}
	return v.(*client)
}

func ReportError(ctx context.Context, err error, opts ...trace.EventOption) {
	activeClient().ReportError(ctx, err, opts...)
}

func ReportPanic(ctx context.Context) {
	activeClient().ReportPanic(ctx)
}

func Shutdown(ctx context.Context) error {
	return activeClient().Shutdown(ctx)
}

func ForceFlush(ctx context.Context) error {
	return activeClient().ForceFlush(ctx)
}

func TracerProvider() *sdktrace.TracerProvider {
	return activeClient().tp
}
