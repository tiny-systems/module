package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"
	"github.com/tiny-systems/otel-collector/pkg/api-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// StatsEvent represents a statistics event
type StatsEvent struct {
	Metric   string
	Value    float64
	Datetime int64
	Element  string // Edge/node/component identifier
}

// StatsEventHandler is a callback for processing stats events
type StatsEventHandler func(events []StatsEvent)

// StatsSubscription manages a stats stream subscription
type StatsSubscription struct {
	ProjectID string
	FlowID    string
	Metrics   []string
	Handler   StatsEventHandler
}

// StatsClient provides methods for streaming statistics from otel-collector
type StatsClient struct {
	conn   *grpc.ClientConn
	client api.StatisticsServiceClient
}

// NewStatsClient creates a new stats client connected to the given address
func NewStatsClient(address string) (*StatsClient, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to stats service: %w", err)
	}

	return &StatsClient{
		conn:   conn,
		client: api.NewStatisticsServiceClient(conn),
	}, nil
}

// Close closes the stats client connection
func (c *StatsClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Subscribe starts streaming stats for the given subscription
// This function blocks until the context is cancelled or an unrecoverable error occurs
// It automatically retries on transient errors using exponential backoff
func (c *StatsClient) Subscribe(ctx context.Context, sub StatsSubscription) error {
	return backoff.Retry(func() error {
		stream, err := c.client.GetStream(ctx, &api.StatisticsStreamRequest{
			ProjectID: sub.ProjectID,
			FlowID:    sub.FlowID,
			Metrics:   sub.Metrics,
		})
		if err != nil {
			// Check if context is cancelled - don't retry
			if ctx.Err() != nil || status.Code(err) == codes.Canceled {
				return backoff.Permanent(err)
			}
			log.Warn().Err(err).Msg("failed to start stats stream, retrying")
			return err
		}

		for {
			select {
			case <-ctx.Done():
				return backoff.Permanent(ctx.Err())
			default:
			}

			res, err := stream.Recv()
			if err != nil {
				code := status.Code(err)
				if ctx.Err() != nil || code == codes.Canceled {
					return backoff.Permanent(nil)
				}
				log.Warn().Err(err).Msg("stats stream error, retrying")
				return err
			}

			// Convert and forward events
			if sub.Handler != nil && len(res.GetEvents()) > 0 {
				events := make([]StatsEvent, len(res.GetEvents()))
				for i, e := range res.GetEvents() {
					events[i] = StatsEvent{
						Metric:   e.Metric,
						Value:    e.Value,
						Datetime: e.Datetime,
						Element:  e.Element,
					}
				}
				sub.Handler(events)
			}
		}
	}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx))
}

// SubscribeEdgeBusy is a convenience method to subscribe to edge busy stats
func (c *StatsClient) SubscribeEdgeBusy(ctx context.Context, projectID, flowID string, handler StatsEventHandler) error {
	return c.Subscribe(ctx, StatsSubscription{
		ProjectID: projectID,
		FlowID:    flowID,
		Metrics:   []string{EdgeBusyStatKey},
		Handler:   handler,
	})
}

// EdgeStatsUpdate represents an edge stats update for the flow editor
type EdgeStatsUpdate struct {
	EdgeID    string
	Timestamp time.Time
}

// ParseEdgeBusyEvent parses an edge busy stats event into an EdgeStatsUpdate
// The metric format is expected to be "tiny_edge_busy" with the edge ID embedded
func ParseEdgeBusyEvent(event StatsEvent) *EdgeStatsUpdate {
	if event.Metric != EdgeBusyStatKey {
		return nil
	}

	// The value is the Unix timestamp in seconds
	timestamp := time.Unix(int64(event.Value), 0)

	// The event.Datetime is Unix milliseconds
	if event.Datetime > 0 {
		timestamp = time.UnixMilli(event.Datetime)
	}

	return &EdgeStatsUpdate{
		EdgeID:    "", // Edge ID would need to be extracted from labels or separate field
		Timestamp: timestamp,
	}
}

// TraceInfo represents a trace summary
type TraceInfo struct {
	ID       string `json:"id"`
	Spans    int64  `json:"spans"`
	Errors   int64  `json:"errors"`
	Data     int64  `json:"data"`
	Length   int64  `json:"length"`
	Duration int64  `json:"duration"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
}

// GetTracesRequest contains parameters for fetching traces
type GetTracesRequest struct {
	ProjectID string
	FlowID    string
	Start     time.Time
	End       time.Time
	Offset    int64
}

// GetTracesResponse contains the list of traces
type GetTracesResponse struct {
	Traces []TraceInfo
	Total  int64
	Offset int64
}

// GetTraces fetches a list of traces from the otel-collector
func (c *StatsClient) GetTraces(ctx context.Context, req GetTracesRequest) (*GetTracesResponse, error) {
	apiReq := &api.StatisticsGetTracesRequest{
		ProjectID: req.ProjectID,
		FlowID:    req.FlowID,
		Offset:    req.Offset,
	}

	if !req.Start.IsZero() {
		apiReq.Start = timestamppb.New(req.Start)
	}
	if !req.End.IsZero() {
		apiReq.End = timestamppb.New(req.End)
	}

	resp, err := c.client.GetTraces(ctx, apiReq)
	if err != nil {
		return nil, fmt.Errorf("get traces failed: %w", err)
	}

	traces := make([]TraceInfo, len(resp.Traces))
	for i, t := range resp.Traces {
		traces[i] = TraceInfo{
			ID:       t.ID,
			Spans:    t.Spans,
			Errors:   t.Errors,
			Data:     t.Data,
			Length:   t.Length,
			Duration: t.Duration,
			Start:    t.Start,
			End:      t.End,
		}
	}

	return &GetTracesResponse{
		Traces: traces,
		Total:  resp.Total,
		Offset: resp.Offset,
	}, nil
}

// SpanAttribute represents an attribute on a span
type SpanAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SpanEvent represents an event on a span
type SpanEvent struct {
	Name       string          `json:"name"`
	Attributes []SpanAttribute `json:"attributes"`
}

// Span represents a trace span with its attributes and events
type Span struct {
	TraceID           string          `json:"trace_id"`
	SpanID            string          `json:"span_id"`
	Name              string          `json:"name"`
	StartTimeUnixNano int64           `json:"start_time_unix_nano"`
	EndTimeUnixNano   int64           `json:"end_time_unix_nano"`
	Attributes        []SpanAttribute `json:"attributes"`
	Events            []SpanEvent     `json:"events"`
}

// TraceData represents a trace with its spans
type TraceData struct {
	TraceID string `json:"trace_id"`
	Spans   []Span `json:"spans"`
}

// GetTraceByID fetches a trace by its ID from the otel-collector
func (c *StatsClient) GetTraceByID(ctx context.Context, projectID, traceID string) (*TraceData, error) {
	resp, err := c.client.GetTraceByID(ctx, &api.GetTraceByIDRequest{
		TraceId:   traceID,
		ProjectId: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get trace by ID failed: %w", err)
	}

	// Convert API spans to SDK spans
	spans := make([]Span, len(resp.Spans))
	for i, s := range resp.Spans {
		attrs := make([]SpanAttribute, len(s.Attributes))
		for j, a := range s.Attributes {
			attrs[j] = SpanAttribute{
				Key:   a.Key,
				Value: a.Value.GetStringValue(),
			}
		}

		events := make([]SpanEvent, len(s.Events))
		for j, e := range s.Events {
			eventAttrs := make([]SpanAttribute, len(e.Attributes))
			for k, ea := range e.Attributes {
				eventAttrs[k] = SpanAttribute{
					Key:   ea.Key,
					Value: ea.Value.GetStringValue(),
				}
			}
			events[j] = SpanEvent{
				Name:       e.Name,
				Attributes: eventAttrs,
			}
		}

		spans[i] = Span{
			TraceID:           string(s.TraceId),
			SpanID:            string(s.SpanId),
			Name:              s.Name,
			StartTimeUnixNano: int64(s.StartTimeUnixNano),
			EndTimeUnixNano:   int64(s.EndTimeUnixNano),
			Attributes:        attrs,
			Events:            events,
		}
	}

	return &TraceData{
		TraceID: traceID,
		Spans:   spans,
	}, nil
}

// ExtractRuntimeDataFromTrace extracts runtime data from trace spans for port inspection
// Returns a map of port names to their JSON-encoded data
func ExtractRuntimeDataFromTrace(trace *TraceData) map[string][]byte {
	if trace == nil {
		return nil
	}

	runtimeData := make(map[string][]byte)
	for _, span := range trace.Spans {
		var port string

		// Extract port from span attributes
		for _, attr := range span.Attributes {
			if attr.Key == "port" {
				port = attr.Value
				break
			}
		}

		if port == "" {
			continue
		}

		// Search for data in events
		for _, event := range span.Events {
			if event.Name != "data" {
				continue
			}

			for _, attr := range event.Attributes {
				if attr.Key == "payload" && attr.Value != "" {
					runtimeData[port] = []byte(attr.Value)
					break
				}
			}
		}
	}

	return runtimeData
}

// ProcessStatsEvent processes a stats event and updates the stats map for an element
// Returns true if the stats were updated
func ProcessStatsEvent(stats map[string]interface{}, event StatsEvent) bool {
	if stats == nil {
		return false
	}

	// Update the stat value - for edge_busy this is a timestamp
	currentVal, exists := stats[event.Metric]
	if exists {
		// Only update if the new value is newer
		currentTs, ok := currentVal.(float64)
		if ok && event.Value <= currentTs {
			return false
		}
	}

	stats[event.Metric] = event.Value
	return true
}

// ShouldAnimateEdge determines if an edge should be animated based on its last activity timestamp
func ShouldAnimateEdge(lastActivityTimestamp float64, nowSeconds float64) bool {
	if lastActivityTimestamp == 0 {
		return false
	}

	timeSinceActivity := nowSeconds - lastActivityTimestamp
	return timeSinceActivity < EdgeAnimationTimeout.Seconds()
}
