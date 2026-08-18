package runner

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/api/v1alpha1"
	m "github.com/tiny-systems/module/module"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newRecordingRunner returns a runner whose spans are captured in memory, plus
// the recorder holding them.
func newRecordingRunner(t *testing.T) (*Runner, *tracetest.SpanRecorder) {
	t.Helper()

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	cmp := &mockComponent{
		ports: []m.Port{
			{Name: v1alpha1.ErrorPort, Label: "Error", Source: true},
			{Name: "response", Label: "Response", Source: true},
		},
	}

	r := NewRunner(cmp).
		SetLogger(logr.Discard()).
		SetTracer(tp.Tracer("test"))
	r.name = "node-a"

	return r, rec
}

// spanEvent finds the single span carrying an event with the given name and
// returns that event's attributes as a plain map.
func spanEvent(t *testing.T, rec *tracetest.SpanRecorder, eventName string) (map[string]string, bool) {
	t.Helper()

	for _, s := range rec.Ended() {
		for _, e := range s.Events() {
			if e.Name != eventName {
				continue
			}
			attrs := make(map[string]string, len(e.Attributes))
			for _, a := range e.Attributes {
				attrs[string(a.Key)] = a.Value.Emit()
			}
			return attrs, true
		}
	}
	return nil, false
}

// An emission on the error port is a caught failure. Error counting keys off
// `error`/`exception` span events, so without one the component that catches a
// failure and routes it out its error port reports zero errors — healthier than
// the same component with the port off, which returns module.Fail and gets
// counted. Enabling a recovery boundary must not hide what it caught.
func TestDataHandler_ErrorPortEmitsErrorEvent(t *testing.T) {
	r, rec := newRecordingRunner(t)

	handler := r.DataHandler(noopHandler)
	res := handler(context.Background(), v1alpha1.ErrorPort, m.ErrorMessage{
		Context: map[string]any{"id": 7},
		Error:   "bucket does not exist",
	})
	if err := res.Err(); err != nil {
		t.Fatalf("error-port emission should itself succeed, got %v", err)
	}

	attrs, ok := spanEvent(t, rec, "error")
	if !ok {
		t.Fatal("no `error` span event recorded — an error-port emission stays uncounted")
	}

	if got := attrs["exception.message"]; got != "bucket does not exist" {
		t.Errorf("exception.message = %q, want the message from the payload", got)
	}
	if got := attrs["handled"]; got != "true" {
		t.Errorf("handled = %q, want \"true\" — a caught error must be distinguishable from a crash", got)
	}
	if got := attrs["port"]; got != "node-a:error" {
		t.Errorf("port = %q, want the full port name", got)
	}
}

// A normal output port must stay uncounted, or every successful emission would
// register as an error.
func TestDataHandler_NormalPortEmitsNoErrorEvent(t *testing.T) {
	r, rec := newRecordingRunner(t)

	handler := r.DataHandler(noopHandler)
	handler(context.Background(), "response", map[string]any{"error": "this is data, not a failure"})

	if attrs, ok := spanEvent(t, rec, "error"); ok {
		t.Fatalf("normal port emission produced an `error` event: %v", attrs)
	}
	if _, ok := spanEvent(t, rec, "data"); !ok {
		t.Error("normal port emission lost its `data` event")
	}
}

// The error port still records payload data — the recovery flow's input is
// visible in the trace exactly like any other emission.
func TestDataHandler_ErrorPortKeepsDataEvent(t *testing.T) {
	r, rec := newRecordingRunner(t)

	handler := r.DataHandler(noopHandler)
	handler(context.Background(), v1alpha1.ErrorPort, m.ErrorMessage{Error: "boom"})

	if _, ok := spanEvent(t, rec, "data"); !ok {
		t.Error("error port emission lost its `data` event")
	}
}

// The payload on an error port is arbitrary component data. An unexpected shape
// must degrade to a generic message: a vague message still counts as an error,
// a panic here would take down the emit path.
func TestErrorPayloadMessage_Shapes(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"canonical ErrorMessage", `{"error":"upstream 503","retryable":true}`, "upstream 503"},
		{"context alongside", `{"context":{"id":1},"error":"denied"}`, "denied"},
		{"empty message", `{"error":""}`, "component emitted on its error port"},
		{"no error field", `{"code":500}`, "component emitted on its error port"},
		{"error is an object", `{"error":{"nested":"x"}}`, "component emitted on its error port"},
		{"bare string payload", `"just a string"`, "component emitted on its error port"},
		{"array payload", `[1,2,3]`, "component emitted on its error port"},
		{"null payload", `null`, "component emitted on its error port"},
		{"not json at all", `<xml/>`, "component emitted on its error port"},
		{"empty bytes", ``, "component emitted on its error port"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorPayloadMessage([]byte(tt.payload)); got != tt.want {
				t.Errorf("errorPayloadMessage(%s) = %q, want %q", tt.payload, got, tt.want)
			}
		})
	}
}
