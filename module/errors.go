package module

import (
	"errors"

	perrors "github.com/tiny-systems/module/pkg/errors"
)

// This file defines the SDK-level error contract so retryability and the
// error-port shape are TYPES, not a per-component convention. Before this, the
// retry component coupled to whatever a component happened to name its fields
// (`retryable`, `context`, `error`), and every component hand-rolled its own
// error struct — a first-party convention no third-party module author could
// discover or be checked against. Now:
//
//   - A failure carries its own retryability via RetryableError (marked with
//     Retryable/Permanent, read with IsRetryable). The code that understands
//     the failure decides — a provider that sees a 5xx marks it retryable —
//     and the decision survives bubbling up through Fail/Result.Err.
//   - An error port emits the one canonical ErrorMessage shape, built with
//     NewError, which derives Retryable from the error itself.
//
// A module that returns module.Retryable(err) on transient failures and emits
// module.NewError(ctx, err) on its error port conforms by construction — the
// retry component and the platform understand it with no shared doc.

// RetryableError is an error that knows whether retrying the operation that
// produced it could succeed. Transient failures (5xx, 429, a dropped
// connection, a timeout) are retryable; permanent ones (4xx, a validation
// error, a constraint violation) are not.
type RetryableError interface {
	error
	Retryable() bool
}

type retryableError struct {
	err       error
	retryable bool
}

func (e retryableError) Error() string   { return e.err.Error() }
func (e retryableError) Unwrap() error   { return e.err }
func (e retryableError) Retryable() bool { return e.retryable }

// Retryable marks err as safe to retry — wrap a transient failure with it
// before returning it or handing it to an error port, so a backoff retry can
// clear it. A nil err stays nil.
//
//	if resp.StatusCode >= 500 {
//	    return module.Fail(module.Retryable(fmt.Errorf("upstream 5xx: %s", body)))
//	}
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return retryableError{err: err, retryable: true}
}

// Permanent marks err as NOT retryable. Optional — a plain error already
// defaults to not-retryable — but explicit at a 4xx/validation branch reads
// clearer and survives a later IsRetryable check unambiguously.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return retryableError{err: err, retryable: false}
}

// IsRetryable reports whether err, or anything it wraps, is a RetryableError
// marked retryable. Plain errors are not retryable.
func IsRetryable(err error) bool {
	var re RetryableError
	if errors.As(err, &re) {
		return re.Retryable()
	}
	return false
}

// ShouldRetry is the single answer to "may this failure be re-attempted?", used
// by every layer that retries: the scheduler's edge dispatch and the retry
// component alike. One predicate so a component declares retryability once and
// both layers agree — before this the scheduler knew only pkg/errors and had
// never heard of IsRetryable, so a component that correctly marked a 500
// retryable got nothing at the edge.
//
// Unmarked errors return false. That default is deliberate: re-attempting a hop
// whose side effect already landed duplicates it — a restart restarts twice, an
// INSERT inserts twice, a paid completion bills twice. Retrying everything by
// default is exactly the behaviour removed in May 2026 after it burned money on
// a storm of unauthorized LLM calls. A component opts its transient failures in
// with Retryable; everything else is left alone.
//
// pkg/errors is honoured for compatibility — a permanent error marked there wins
// over any retryable marking, since a caller went out of its way to forbid the
// retry.
func ShouldRetry(err error) bool {
	if err == nil {
		return false
	}
	if perrors.IsPermanent(err) {
		return false
	}
	return IsRetryable(err)
}

// ErrorMessage is the canonical payload every component's error port should
// emit. Using this type (rather than a hand-rolled struct) guarantees the
// {context, error, retryable} shape the retry component and the platform
// expect, so a module conforms by construction instead of by copying field
// names out of a doc.
type ErrorMessage struct {
	Context   any    `json:"context,omitempty" configurable:"true" title:"Context" description:"Original request payload — passed through unchanged so a recovery flow, or the retry component, can re-invoke the upstream."`
	Error     string `json:"error" title:"Error" description:"Human-readable failure message."`
	Retryable bool   `json:"retryable" title:"Retryable" description:"True when a backoff retry could clear the failure (transient: 5xx, 429, dropped connection, timeout). Derived from the error via module.Retryable / module.IsRetryable."`
}

// NewError builds the canonical error-port payload, deriving Retryable from the
// error itself. Emit it from an error port:
//
//	return handler(ctx, ErrorPort, module.NewError(reqContext, err))
//
// Wrap the error with module.Retryable at the point you know it's transient;
// NewError picks that up so downstream (the retry component) sees it.
func NewError(ctx any, err error) ErrorMessage {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return ErrorMessage{Context: ctx, Error: msg, Retryable: IsRetryable(err)}
}
