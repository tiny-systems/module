package errors

import "errors"

// PermanentError wraps an error to indicate it should not be retried.
// Components can return this to signal that an operation has failed permanently
// (e.g., validation error, business logic error) and should not be retried.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

// NewPermanentError creates a new permanent error that will not be retried.
func NewPermanentError(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent checks if an error is a permanent error that should not be retried.
func IsPermanent(err error) bool {
	if err == nil {
		return false
	}
	var perr *PermanentError
	return errors.As(err, &perr)
}

// CodedError carries a stable string code alongside the wrapped error
// so the scheduler's edge-retry loop can match the code against the
// edge's NonRetryableErrorCodes list. Codes are author-defined
// strings — recommended conventions: "quota_exceeded",
// "unauthorized", "content_filter", "validation".
type CodedError struct {
	Code string
	Err  error
}

func (e *CodedError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *CodedError) Unwrap() error { return e.Err }

// NonRetryable wraps err with a permanent-on-this-code marker.
// Equivalent to NewPermanentError plus a stable code the wire can
// stamp on the reply via the x-error-code header. The scheduler's
// edge-retry loop will skip retry when the code matches the edge's
// NonRetryableErrorCodes regardless of MaxAttempts.
func NonRetryable(code string, err error) error {
	if err == nil {
		return nil
	}
	return &CodedError{Code: code, Err: err}
}

// ErrorCode extracts the code from an error chain. Returns "" when
// no CodedError is in the chain.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}
