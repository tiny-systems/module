package module

// Result is the typed return of every Handler call and Component.Handle
// invocation. It carries either a successful payload, an error, or nothing
// (a successful no-op). Components construct Result via Ok / Fail; the
// runner unwraps it via Err / Value.
//
// Why this exists: handler returns must propagate up through the call chain
// to support blocking I/O — the canonical example is http-server, which
// hands a request to its downstream port and waits on the synchronous
// response that flows back. Previously the return was `any`, which let
// authors silently drop it with `_ = handler(...)` or a bare call. Lost
// returns mean lost responses and timed-out requests three days later.
//
// Result is a value type (not a pointer): a zero Result is a valid
// "successful no-op" and equality comparisons work as expected.
type Result struct {
	inner any
}

// Ok wraps a successful payload. The payload may be nil for "delivered
// successfully, no response data" (the common case for fire-and-forget
// emits to non-blocking ports).
func Ok(v any) Result {
	return Result{inner: v}
}

// Fail wraps an error. A nil error becomes a zero Result so callers can
// safely Fail(maybeNilErr) without producing a phantom-error result.
func Fail(err error) Result {
	if err == nil {
		return Result{}
	}
	return Result{inner: err}
}

// Pass wraps whatever the value is, treating error-typed values as
// failures. Bridge for sites that still receive untyped any from legacy
// chains during migration. New code should use Ok / Fail directly.
func Pass(v any) Result {
	return Result{inner: v}
}

// Err returns the wrapped error, or nil if the result is a success.
// This is what the runner calls to decide retry vs commit.
func (r Result) Err() error {
	if err, ok := r.inner.(error); ok {
		return err
	}
	return nil
}

// Value returns the wrapped payload, or nil if the result is a failure.
// Callers wanting both payload and error should call Err first; if Err
// is non-nil, Value returns nil.
func (r Result) Value() any {
	if _, ok := r.inner.(error); ok {
		return nil
	}
	return r.inner
}

// IsErr reports whether the result wraps an error.
func (r Result) IsErr() bool {
	_, ok := r.inner.(error)
	return ok
}
