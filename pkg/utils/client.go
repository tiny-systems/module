package utils

import (
	"context"
	"sync"
	"time"
)

// PortForwardRequest contains parameters for port forwarding
type PortForwardRequest struct {
	Namespace   string
	ServiceName string
	Port        int
}

// ClientInterface abstracts the otel-collector address retrieval
// Desktop client returns a port-forwarded address, website may use direct access
type ClientInterface interface {
	// GetForwardedAddress returns the address to connect to the otel-collector
	// For desktop client, this creates a port-forward and returns localhost:port
	// For website, this might return a direct address
	GetForwardedAddress(ctx context.Context, req PortForwardRequest, alias string) (string, error)
}

// ConstantAddressClient is a simple implementation that returns a constant address
// Useful for testing or when the address is already known
type ConstantAddressClient struct {
	Address string
}

// GetForwardedAddress returns the constant address
func (c *ConstantAddressClient) GetForwardedAddress(ctx context.Context, req PortForwardRequest, alias string) (string, error) {
	return c.Address, nil
}

// TraceService provides methods for loading trace data
type TraceService struct {
	client        ClientInterface
	otelService   string
	otelPort      int
	statsClient   *StatsClient
	statsClientMu sync.Mutex
}

// TraceServiceConfig contains configuration for the trace service
type TraceServiceConfig struct {
	Client      ClientInterface
	OtelService string
	OtelPort    int
}

// DefaultOtelService is the default otel-collector service name
const DefaultOtelService = "tinysystems-otel-collector"

// DefaultOtelPort is the default otel-collector port
const DefaultOtelPort = 2345

// NewTraceService creates a new trace service
func NewTraceService(cfg TraceServiceConfig) *TraceService {
	if cfg.OtelService == "" {
		cfg.OtelService = DefaultOtelService
	}
	if cfg.OtelPort == 0 {
		cfg.OtelPort = DefaultOtelPort
	}

	return &TraceService{
		client:      cfg.Client,
		otelService: cfg.OtelService,
		otelPort:    cfg.OtelPort,
	}
}

// getOrCreateStatsClient returns the stats client, creating it if necessary
func (s *TraceService) getOrCreateStatsClient(ctx context.Context, namespace string) (*StatsClient, error) {
	s.statsClientMu.Lock()
	defer s.statsClientMu.Unlock()

	if s.statsClient != nil {
		return s.statsClient, nil
	}

	// Get the otel-collector address
	addr, err := s.client.GetForwardedAddress(ctx, PortForwardRequest{
		Namespace:   namespace,
		ServiceName: s.otelService,
		Port:        s.otelPort,
	}, "otel-collector")
	if err != nil {
		return nil, err
	}

	// Create the stats client
	statsClient, err := NewStatsClient(addr)
	if err != nil {
		return nil, err
	}

	s.statsClient = statsClient
	return statsClient, nil
}

// Close closes the trace service and its connections
func (s *TraceService) Close() error {
	s.statsClientMu.Lock()
	defer s.statsClientMu.Unlock()

	if s.statsClient != nil {
		err := s.statsClient.Close()
		s.statsClient = nil
		return err
	}
	return nil
}

// GetTraces fetches traces for a specific flow
func (s *TraceService) GetTraces(ctx context.Context, namespace, projectName, flowID string, start, end time.Time, offset int64) (*GetTracesResponse, error) {
	statsClient, err := s.getOrCreateStatsClient(ctx, namespace)
	if err != nil {
		return nil, err
	}

	// Default time range: last 15 minutes
	if start.IsZero() {
		start = time.Now().Add(-15 * time.Minute)
	}
	if end.IsZero() {
		end = time.Now()
	}

	req := GetTracesRequest{
		ProjectID: projectName,
		FlowID:    flowID,
		Start:     start,
		End:       end,
		Offset:    offset,
	}

	return statsClient.GetTraces(ctx, req)
}

// GetTraceByID fetches a trace by its ID
func (s *TraceService) GetTraceByID(ctx context.Context, namespace, projectName, traceID string) (*TraceData, error) {
	statsClient, err := s.getOrCreateStatsClient(ctx, namespace)
	if err != nil {
		return nil, err
	}

	return statsClient.GetTraceByID(ctx, projectName, traceID)
}

// LoadTraceRuntimeData fetches trace data and extracts runtime data for port inspection
func (s *TraceService) LoadTraceRuntimeData(ctx context.Context, namespace, projectName, traceID string) (map[string][]byte, error) {
	trace, err := s.GetTraceByID(ctx, namespace, projectName, traceID)
	if err != nil {
		return nil, err
	}

	return ExtractRuntimeDataFromTrace(trace), nil
}

// SubscribeToStats starts streaming stats for a flow
func (s *TraceService) SubscribeToStats(ctx context.Context, namespace, projectID, flowID string, handler StatsEventHandler) error {
	statsClient, err := s.getOrCreateStatsClient(ctx, namespace)
	if err != nil {
		return err
	}

	return statsClient.SubscribeEdgeBusy(ctx, projectID, flowID, handler)
}
