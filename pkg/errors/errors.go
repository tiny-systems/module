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
