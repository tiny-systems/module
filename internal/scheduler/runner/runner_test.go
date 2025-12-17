package runner

import (
	"context"
	"errors"
	"github.com/go-logr/logr"
	"github.com/tiny-systems/module/api/v1alpha1"
	perrors "github.com/tiny-systems/module/pkg/errors"
	"testing"
	"time"
)

// mockHandler creates a handler that fails N times before succeeding
func mockHandler(failCount int, err error, result any) Handler {
	attempts := 0
	return func(ctx context.Context, msg *Msg) (any, error) {
		attempts++
		if attempts <= failCount {
			return nil, err
		}
		return result, nil
	}
}

// createTestRunner creates a minimal runner for testing
func createTestRunner() *Runner {
	runner := &Runner{
		log: logr.Discard(), // Use discard logger for tests
	}
	return runner
}

func TestRunner_sendToEdgeWithRetry_Success(t *testing.T) {
	runner := createTestRunner()
	ctx := context.Background()

	tests := []struct {
		name      string
		handler   Handler
		wantErr   bool
		wantRetry bool
	}{
		{
			name: "success on first attempt",
			handler: func(ctx context.Context, msg *Msg) (any, error) {
				return "success", nil
			},
			wantErr:   false,
			wantRetry: false,
		},
		{
			name:      "success after 2 failures",
			handler:   mockHandler(2, errors.New("transient error"), "success"),
			wantErr:   false,
			wantRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edge := v1alpha1.TinyNodeEdge{
				ID:   "test-edge",
				To:   "target-node:port",
				Port: "output",
			}

			result, err := runner.sendToEdgeWithRetry(
				ctx,
				edge,
				"source-node:output",
				"target-node:port",
				[]byte(`{"test":"data"}`),
				nil,
				tt.handler,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("sendToEdgeWithRetry() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result == nil {
				t.Error("sendToEdgeWithRetry() expected non-nil result on success")
			}
		})
	}
}

func TestRunner_sendToEdgeWithRetry_PermanentError(t *testing.T) {
	runner := createTestRunner()
	ctx := context.Background()

	// Handler that returns permanent error
	handler := func(ctx context.Context, msg *Msg) (any, error) {
		return nil, perrors.NewPermanentError(errors.New("validation failed"))
	}

	edge := v1alpha1.TinyNodeEdge{
		ID:   "test-edge",
		To:   "target-node:port",
		Port: "output",
	}

	start := time.Now()
	result, err := runner.sendToEdgeWithRetry(
		ctx,
		edge,
		"source-node:output",
		"target-node:port",
		[]byte(`{"test":"data"}`),
		nil,
		handler,
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("sendToEdgeWithRetry() expected error for permanent error")
	}

	if !perrors.IsPermanent(err) {
		t.Error("sendToEdgeWithRetry() should return permanent error")
	}

	if result != nil {
		t.Error("sendToEdgeWithRetry() expected nil result on permanent error")
	}

	// Should return immediately without retries (< 500ms)
	if elapsed > 500*time.Millisecond {
		t.Errorf("sendToEdgeWithRetry() took %v, expected immediate return for permanent error", elapsed)
	}
}

func TestRunner_sendToEdgeWithRetry_ContextCancellation(t *testing.T) {
	runner := createTestRunner()
	ctx, cancel := context.WithCancel(context.Background())

	// Handler that always fails
	handler := func(ctx context.Context, msg *Msg) (any, error) {
		return nil, errors.New("always fails")
	}

	edge := v1alpha1.TinyNodeEdge{
		ID:   "test-edge",
		To:   "target-node:port",
		Port: "output",
	}

	// Cancel context after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result, err := runner.sendToEdgeWithRetry(
		ctx,
		edge,
		"source-node:output",
		"target-node:port",
		[]byte(`{"test":"data"}`),
		nil,
		handler,
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("sendToEdgeWithRetry() expected error when context cancelled")
	}

	if result != nil {
		t.Error("sendToEdgeWithRetry() expected nil result when context cancelled")
	}

	// Should return shortly after cancellation
	if elapsed > 2*time.Second {
		t.Errorf("sendToEdgeWithRetry() took %v, expected faster return on context cancellation", elapsed)
	}
}

func TestRunner_sendToEdgeWithRetry_ExponentialBackoff(t *testing.T) {
	runner := createTestRunner()
	ctx := context.Background()

	callTimes := make([]time.Time, 0)
	handler := func(ctx context.Context, msg *Msg) (any, error) {
		callTimes = append(callTimes, time.Now())
		if len(callTimes) < 4 {
			return nil, errors.New("transient error")
		}
		return "success", nil
	}

	edge := v1alpha1.TinyNodeEdge{
		ID:   "test-edge",
		To:   "target-node:port",
		Port: "output",
	}

	_, err := runner.sendToEdgeWithRetry(
		ctx,
		edge,
		"source-node:output",
		"target-node:port",
		[]byte(`{"test":"data"}`),
		nil,
		handler,
	)

	if err != nil {
		t.Fatalf("sendToEdgeWithRetry() unexpected error: %v", err)
	}

	if len(callTimes) != 4 {
		t.Fatalf("Expected 4 attempts, got %d", len(callTimes))
	}

	// Check that intervals are increasing (exponential backoff)
	intervals := []time.Duration{
		callTimes[1].Sub(callTimes[0]),
		callTimes[2].Sub(callTimes[1]),
		callTimes[3].Sub(callTimes[2]),
	}

	// Verify backoff is happening (each interval should be at least 500ms)
	// and intervals are generally increasing
	for i, interval := range intervals {
		if interval < 500*time.Millisecond {
			t.Errorf("Retry %d: interval = %v, expected at least 500ms (exponential backoff)", i+1, interval)
		}
	}

	// Verify general trend: later intervals should be longer
	// (allowing for jitter, just check second > first or third > first)
	if intervals[1] < intervals[0] && intervals[2] < intervals[0] {
		t.Error("Backoff intervals are not increasing as expected")
	}
}

func TestRunner_sendToEdgeWithRetry_MessageContent(t *testing.T) {
	runner := createTestRunner()
	ctx := context.Background()

	var receivedMsg *Msg
	handler := func(ctx context.Context, msg *Msg) (any, error) {
		receivedMsg = msg
		return "ok", nil
	}

	edge := v1alpha1.TinyNodeEdge{
		ID:   "edge-123",
		To:   "target-node:input",
		Port: "output",
	}

	testData := []byte(`{"key":"value"}`)
	testResp := "response-config"

	_, err := runner.sendToEdgeWithRetry(
		ctx,
		edge,
		"source-node:output",
		"target-node:input",
		testData,
		testResp,
		handler,
	)

	if err != nil {
		t.Fatalf("sendToEdgeWithRetry() unexpected error: %v", err)
	}

	if receivedMsg == nil {
		t.Fatal("Handler did not receive message")
	}

	if receivedMsg.To != "target-node:input" {
		t.Errorf("Message.To = %v, want %v", receivedMsg.To, "target-node:input")
	}

	if receivedMsg.From != "source-node:output" {
		t.Errorf("Message.From = %v, want %v", receivedMsg.From, "source-node:output")
	}

	if receivedMsg.EdgeID != "edge-123" {
		t.Errorf("Message.EdgeID = %v, want %v", receivedMsg.EdgeID, "edge-123")
	}

	if string(receivedMsg.Data) != string(testData) {
		t.Errorf("Message.Data = %v, want %v", string(receivedMsg.Data), string(testData))
	}

	if receivedMsg.Resp != testResp {
		t.Errorf("Message.Resp = %v, want %v", receivedMsg.Resp, testResp)
	}
}

func TestRunner_sendToEdgeWithRetry_NilHandler(t *testing.T) {
	runner := createTestRunner()
	ctx := context.Background()

	edge := v1alpha1.TinyNodeEdge{
		ID:   "test-edge",
		To:   "target-node:port",
		Port: "output",
	}

	// This should panic or handle gracefully
	defer func() {
		if r := recover(); r != nil {
			// Expected panic from nil handler - this is OK
		}
	}()

	_, _ = runner.sendToEdgeWithRetry(
		ctx,
		edge,
		"source-node:output",
		"target-node:port",
		[]byte(`{"test":"data"}`),
		nil,
		nil, // nil handler
	)
}

func TestRunner_sendToEdgeWithRetry_TransientToSuccess(t *testing.T) {
	runner := createTestRunner()
	ctx := context.Background()

	attempts := 0
	handler := func(ctx context.Context, msg *Msg) (any, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("network timeout")
		}
		if attempts == 2 {
			return nil, errors.New("connection refused")
		}
		return "finally worked", nil
	}

	edge := v1alpha1.TinyNodeEdge{
		ID:   "test-edge",
		To:   "target-node:port",
		Port: "output",
	}

	result, err := runner.sendToEdgeWithRetry(
		ctx,
		edge,
		"source-node:output",
		"target-node:port",
		[]byte(`{"test":"data"}`),
		nil,
		handler,
	)

	if err != nil {
		t.Errorf("sendToEdgeWithRetry() unexpected error: %v", err)
	}

	if result != "finally worked" {
		t.Errorf("sendToEdgeWithRetry() result = %v, want %v", result, "finally worked")
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}
