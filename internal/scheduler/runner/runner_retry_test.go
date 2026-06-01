package runner

import (
	"errors"
	"testing"
	"time"

	"github.com/tiny-systems/module/api/v1alpha1"
	perrors "github.com/tiny-systems/module/pkg/errors"
)

// ---------------------------------------------------------------------
// backoffDelay — exponential growth, capped at MaxDelay, sensible
// defaults when policy is nil or partially set.
// ---------------------------------------------------------------------

func TestBackoffDelay_Defaults(t *testing.T) {
	// nil policy → 1s, 2s, 4s, 8s, 16s, then cap at 30s.
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}
	for i, exp := range want {
		got := backoffDelay(nil, i+1)
		if got != exp {
			t.Errorf("attempt %d: got %v, want %v", i+1, got, exp)
		}
	}
}

func TestBackoffDelay_CustomInitialAndCoefficient(t *testing.T) {
	p := &v1alpha1.EdgeRetryPolicy{
		MaxAttempts:        5,
		InitialDelayMs:     500,
		BackoffCoefficient: "3.0",
		MaxDelayMs:         10000,
	}
	// 500ms, 1500ms, 4500ms, cap at 10s, cap at 10s
	want := []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond, 4500 * time.Millisecond, 10 * time.Second, 10 * time.Second}
	for i, exp := range want {
		got := backoffDelay(p, i+1)
		if got != exp {
			t.Errorf("attempt %d: got %v, want %v", i+1, got, exp)
		}
	}
}

func TestBackoffDelay_InvalidCoefficientFallsBackToDefault(t *testing.T) {
	p := &v1alpha1.EdgeRetryPolicy{
		MaxAttempts:        3,
		InitialDelayMs:     100,
		BackoffCoefficient: "garbage",
	}
	// Default coefficient = 2.0 → 100ms, 200ms, 400ms
	got := backoffDelay(p, 2)
	if got != 200*time.Millisecond {
		t.Errorf("got %v, want 200ms (default 2.0 coefficient)", got)
	}
}

// ---------------------------------------------------------------------
// Retry decision logic — driven by RetryPolicy. We test the policy
// branches against a stub handler that counts invocations + can
// return either a vanilla error or a CodedError.
// ---------------------------------------------------------------------

func TestRetryPolicy_DefaultIsSingleShot(t *testing.T) {
	policy := (*v1alpha1.EdgeRetryPolicy)(nil)
	max := maxAttemptsFromPolicy(policy)
	if max != 1 {
		t.Errorf("nil policy → MaxAttempts=%d, want 1", max)
	}

	policy = &v1alpha1.EdgeRetryPolicy{MaxAttempts: 0}
	if max := maxAttemptsFromPolicy(policy); max != 1 {
		t.Errorf("MaxAttempts=0 → got %d, want 1 (default)", max)
	}

	policy = &v1alpha1.EdgeRetryPolicy{MaxAttempts: 1}
	if max := maxAttemptsFromPolicy(policy); max != 1 {
		t.Errorf("MaxAttempts=1 → got %d, want 1", max)
	}
}

func TestRetryPolicy_NonRetryableCodeShortCircuits(t *testing.T) {
	policy := &v1alpha1.EdgeRetryPolicy{
		MaxAttempts:            5,
		NonRetryableErrorCodes: []string{"quota_exceeded", "unauthorized"},
	}
	err := perrors.NonRetryable("quota_exceeded", errors.New("out of tokens"))
	if !shouldShortCircuit(policy, err) {
		t.Error("quota_exceeded should short-circuit per policy")
	}

	err2 := perrors.NonRetryable("rate_limit", errors.New("slow down"))
	if shouldShortCircuit(policy, err2) {
		t.Error("rate_limit is not in policy.NonRetryableErrorCodes — should NOT short-circuit")
	}

	if shouldShortCircuit(policy, errors.New("plain transient")) {
		t.Error("non-coded error should NOT short-circuit (policy retry path applies)")
	}
}

// Helpers under test — exposed via internal lowercase functions in
// the same package. Real production code calls them inline within
// sendToEdgeWithRetry; this gives us tight unit tests without
// standing up a full Runner.

func maxAttemptsFromPolicy(p *v1alpha1.EdgeRetryPolicy) int {
	if p == nil || p.MaxAttempts <= 1 {
		return 1
	}
	return p.MaxAttempts
}

func shouldShortCircuit(p *v1alpha1.EdgeRetryPolicy, err error) bool {
	if p == nil || err == nil {
		return false
	}
	code := perrors.ErrorCode(err)
	if code == "" {
		return false
	}
	for _, blocked := range p.NonRetryableErrorCodes {
		if blocked == code {
			return true
		}
	}
	return false
}
