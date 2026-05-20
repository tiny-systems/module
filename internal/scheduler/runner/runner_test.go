package runner

import (
	"context"
	"errors"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
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
		log:         logr.Discard(),
		edgeCancels: cmap.New[context.CancelFunc](),
	}
	return runner
}

// Edge delivery is single-shot since 2026-05-20. These tests verify
// the no-retry behavior: success passes through, error returns
// immediately on the first attempt.

func TestRunner_sendToEdgeWithRetry_Success(t *testing.T) {
	runner := createTestRunner()
	ctx := context.Background()

	handler := func(ctx context.Context, msg *Msg) (any, error) {
		return "success", nil
	}

	edge := v1alpha1.TinyNodeEdge{ID: "test-edge", To: "target-node:port", Port: "output"}

	result, err := runner.sendToEdgeWithRetry(ctx, edge, "source-node:output", "target-node:port", []byte(`{"test":"data"}`), nil, handler)
	if err != nil {
		t.Fatalf("sendToEdgeWithRetry() unexpected error: %v", err)
	}
	if result == nil {
		t.Error("sendToEdgeWithRetry() expected non-nil result on success")
	}
}

func TestRunner_sendToEdgeWithRetry_NoRetryOnTransient(t *testing.T) {
	runner := createTestRunner()
	ctx := context.Background()

	attempts := 0
	handler := func(ctx context.Context, msg *Msg) (any, error) {
		attempts++
		return nil, errors.New("transient error")
	}

	edge := v1alpha1.TinyNodeEdge{ID: "test-edge", To: "target-node:port", Port: "output"}
	start := time.Now()
	_, err := runner.sendToEdgeWithRetry(ctx, edge, "source-node:output", "target-node:port", []byte(`{"test":"data"}`), nil, handler)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("sendToEdgeWithRetry() expected error")
	}
	if attempts != 1 {
		t.Errorf("Expected exactly 1 attempt (no retries), got %d", attempts)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("sendToEdgeWithRetry() took %v, expected immediate single-shot return", elapsed)
	}
}

func TestRunner_sendToEdgeWithRetry_PermanentError(t *testing.T) {
	// PermanentError still surfaces through the layer untouched —
	// callers can still inspect the error with perrors.IsPermanent
	// even though the layer itself no longer retries on transient
	// errors anyway.
	runner := createTestRunner()
	ctx := context.Background()

	handler := func(ctx context.Context, msg *Msg) (any, error) {
		return nil, perrors.NewPermanentError(errors.New("validation failed"))
	}

	edge := v1alpha1.TinyNodeEdge{ID: "test-edge", To: "target-node:port", Port: "output"}
	start := time.Now()
	result, err := runner.sendToEdgeWithRetry(ctx, edge, "source-node:output", "target-node:port", []byte(`{"test":"data"}`), nil, handler)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("sendToEdgeWithRetry() expected error for permanent error")
	}
	if !perrors.IsPermanent(err) {
		t.Error("sendToEdgeWithRetry() should propagate permanent-error wrap")
	}
	if result != nil {
		t.Error("sendToEdgeWithRetry() expected nil result on error")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("sendToEdgeWithRetry() took %v, expected immediate return", elapsed)
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

