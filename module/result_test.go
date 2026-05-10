package module_test

import (
	"errors"
	"testing"

	"github.com/tiny-systems/module/module"
)

func TestResult_OkCarriesValue(t *testing.T) {
	r := module.Ok(42)
	if r.IsErr() {
		t.Fatalf("Ok result reports IsErr=true")
	}
	if got := r.Value(); got != 42 {
		t.Errorf("Value() = %v; want 42", got)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v; want nil", err)
	}
}

func TestResult_OkAcceptsNilPayload(t *testing.T) {
	r := module.Ok(nil)
	if r.IsErr() {
		t.Fatalf("Ok(nil) reports IsErr=true")
	}
	if got := r.Value(); got != nil {
		t.Errorf("Value() = %v; want nil", got)
	}
}

func TestResult_FailCarriesError(t *testing.T) {
	wantErr := errors.New("boom")
	r := module.Fail(wantErr)
	if !r.IsErr() {
		t.Fatalf("Fail result reports IsErr=false")
	}
	if got := r.Err(); !errors.Is(got, wantErr) {
		t.Errorf("Err() = %v; want %v", got, wantErr)
	}
	// On failure, Value must mask the inner error rather than leak it as
	// data — callers that only inspect Value would otherwise miss the
	// failure entirely.
	if got := r.Value(); got != nil {
		t.Errorf("Value() on failure = %v; want nil", got)
	}
}

func TestResult_FailNilErrCollapsesToZero(t *testing.T) {
	r := module.Fail(nil)
	if r.IsErr() {
		t.Errorf("Fail(nil) reports IsErr=true; want false")
	}
	if r.Err() != nil {
		t.Errorf("Fail(nil).Err() = %v; want nil", r.Err())
	}
	if r.Value() != nil {
		t.Errorf("Fail(nil).Value() = %v; want nil", r.Value())
	}
}

func TestResult_PassRoutesByValueType(t *testing.T) {
	wantErr := errors.New("from pass")

	r := module.Pass(wantErr)
	if !r.IsErr() {
		t.Errorf("Pass(error) reports IsErr=false; the migration helper must classify error-typed values as failures")
	}

	r = module.Pass("hello")
	if r.IsErr() {
		t.Errorf("Pass(string) reports IsErr=true; non-error values must classify as success")
	}
	if got := r.Value(); got != "hello" {
		t.Errorf("Pass(\"hello\").Value() = %v; want \"hello\"", got)
	}
}

func TestResult_ZeroIsSuccess(t *testing.T) {
	var r module.Result
	if r.IsErr() {
		t.Errorf("zero Result reports IsErr=true; zero must be valid no-op success")
	}
	if r.Err() != nil {
		t.Errorf("zero Result Err() = %v; want nil", r.Err())
	}
	if r.Value() != nil {
		t.Errorf("zero Result Value() = %v; want nil", r.Value())
	}
}
