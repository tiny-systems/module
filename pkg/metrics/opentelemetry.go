package metrics

import (
	"context"
	"fmt"
	"time"

	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding/gzip"
)

func configureMetrics(ctx context.Context, client *client, conf *config) error {
	exp, err := otlpMetricClient(ctx, conf, client.dsn)
	if err != nil {
		return fmt.Errorf("new otlp client error: %v", err)
	}

	reader := sdkmetric.NewPeriodicReader(
		exp,
		sdkmetric.WithInterval(time.Second),
	)

	providerOptions := append(conf.metricOptions,
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(conf.newResource()),
	)
	provider := sdkmetric.NewMeterProvider(providerOptions...)

	otel.SetMeterProvider(provider)
	client.mp = provider

	if err := runtimemetrics.Start(); err != nil {
		return fmt.Errorf("runtimemetrics.Start failed: %s", err)
	}

	return nil
}

func otlpMetricClient(ctx context.Context, conf *config, dsn *DSN) (sdkmetric.Exporter, error) {
	options := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(dsn.OTLPEndpoint()),
		otlpmetricgrpc.WithHeaders(map[string]string{}),
		otlpmetricgrpc.WithCompressor(gzip.Name),
		otlpmetricgrpc.WithTemporalitySelector(preferDeltaTemporalitySelector),
	}

	if conf.tlsConf != nil {
		creds := credentials.NewTLS(conf.tlsConf)
		options = append(options, otlpmetricgrpc.WithTLSCredentials(creds))
	} else if dsn.Scheme == "https" {
		// Create credentials using system certificates.
		creds := credentials.NewClientTLSFromCert(nil, "")
		options = append(options, otlpmetricgrpc.WithTLSCredentials(creds))
	} else {
		options = append(options, otlpmetricgrpc.WithInsecure())
	}

	return otlpmetricgrpc.New(ctx, options...)
}

func preferDeltaTemporalitySelector(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.DeltaTemporality
}
