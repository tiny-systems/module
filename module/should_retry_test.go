package module

import (
	"fmt"
	"testing"

	perrors "github.com/tiny-systems/module/pkg/errors"
)

func TestShouldRetry(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		// Default-deny: an unmarked failure may already have had its effect, so
		// the scheduler must not repeat the hop.
		{"plain error is not retried", fmt.Errorf("boom"), false},
		{"marked retryable", Retryable(fmt.Errorf("upstream 503")), true},
		{"marked permanent", Permanent(fmt.Errorf("bad request")), false},
		// Wrapping must not lose the marking — errors travel wrapped through
		// Fail/Result.Err before any retry layer sees them.
		{"retryable survives wrapping", fmt.Errorf("ctx: %w", Retryable(fmt.Errorf("503"))), true},
		// pkg/errors is the older vocabulary; a permanent marker there wins.
		{"pkg/errors permanent honoured", perrors.NewPermanentError(fmt.Errorf("unauthorized")), false},
		{"pkg/errors coded non-retryable honoured", perrors.NonRetryable("quota_exceeded", fmt.Errorf("over quota")), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRetry(tc.err); got != tc.want {
				t.Errorf("ShouldRetry(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// A permanent marker must beat a retryable one: whichever layer forbade the
// retry did so deliberately.
func TestPermanentBeatsRetryable(t *testing.T) {
	err := perrors.NewPermanentError(Retryable(fmt.Errorf("503")))
	if ShouldRetry(err) {
		t.Error("a permanent wrapper did not override the retryable marking")
	}
}
