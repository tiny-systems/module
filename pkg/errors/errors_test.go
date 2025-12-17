package errors

import (
	"errors"
	"testing"
)

func TestNewPermanentError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantNil bool
	}{
		{
			name:    "nil error",
			err:     nil,
			wantNil: true,
		},
		{
			name:    "regular error",
			err:     errors.New("test error"),
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPermanentError(tt.err)
			if tt.wantNil {
				if got != nil {
					t.Errorf("NewPermanentError() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Error("NewPermanentError() = nil, want non-nil")
				}
				if !IsPermanent(got) {
					t.Error("NewPermanentError() should create a permanent error")
				}
			}
		})
	}
}

func TestIsPermanent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "regular error",
			err:  errors.New("regular error"),
			want: false,
		},
		{
			name: "permanent error",
			err:  NewPermanentError(errors.New("permanent error")),
			want: true,
		},
		{
			name: "wrapped permanent error",
			err:  NewPermanentError(errors.New("wrapped permanent")),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPermanent(tt.err); got != tt.want {
				t.Errorf("IsPermanent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPermanentError_Error(t *testing.T) {
	err := errors.New("test error message")
	perr := NewPermanentError(err)

	if perr.Error() != "test error message" {
		t.Errorf("PermanentError.Error() = %v, want %v", perr.Error(), "test error message")
	}
}

func TestPermanentError_Unwrap(t *testing.T) {
	originalErr := errors.New("original error")
	perr := NewPermanentError(originalErr)

	unwrapped := errors.Unwrap(perr)
	if unwrapped != originalErr {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, originalErr)
	}
}
