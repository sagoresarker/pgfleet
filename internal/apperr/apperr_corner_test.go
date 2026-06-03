package apperr

import (
	"fmt"
	"io"
	"net/http"
	"testing"
)

// TestKindThroughWrappedChain — errors.As finds the OUTERMOST *Error in a
// chain, so its kind wins.
func TestKindThroughWrappedChain(t *testing.T) {
	chain := Wrap(KindNotFound, "outer", Wrap(KindInvalid, "inner", io.EOF))
	if Kind(chain) != KindNotFound {
		t.Errorf("Kind = %v, want KindNotFound (outermost)", Kind(chain))
	}
}

// TestKindFindsAppErrWrappedByFmt — an *Error wrapped by fmt.Errorf(%w) is
// still classified correctly.
func TestKindFindsAppErrWrappedByFmt(t *testing.T) {
	wrapped := fmt.Errorf("context: %w", New(KindForbidden, "denied"))
	if Kind(wrapped) != KindForbidden {
		t.Errorf("Kind = %v, want KindForbidden", Kind(wrapped))
	}
}

// TestKindAndStatusOfNil — nil classifies as Internal/500.
func TestKindAndStatusOfNil(t *testing.T) {
	if Kind(nil) != KindInternal {
		t.Errorf("Kind(nil) = %v, want KindInternal", Kind(nil))
	}
	if HTTPStatus(nil) != http.StatusInternalServerError {
		t.Errorf("HTTPStatus(nil) = %d, want 500", HTTPStatus(nil))
	}
}

// TestWrapNilCause — Wrap with a nil cause has no ": <nil>" suffix and a nil
// Unwrap.
func TestWrapNilCause(t *testing.T) {
	e := Wrap(KindInvalid, "just the message", nil)
	if e.Error() != "just the message" {
		t.Errorf("Error() = %q", e.Error())
	}
	if e.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", e.Unwrap())
	}
}

// TestHTTPStatusMatrix — every kind maps to its status; unknown -> 500.
func TestHTTPStatusMatrix(t *testing.T) {
	cases := map[Code]int{
		KindInternal:     http.StatusInternalServerError,
		KindInvalid:      http.StatusBadRequest,
		KindUnauthorized: http.StatusUnauthorized,
		KindForbidden:    http.StatusForbidden,
		KindNotFound:     http.StatusNotFound,
		KindConflict:     http.StatusConflict,
	}
	for code, want := range cases {
		if got := HTTPStatus(New(code, "x")); got != want {
			t.Errorf("HTTPStatus(%v) = %d, want %d", code, got, want)
		}
	}
	if HTTPStatus(fmt.Errorf("plain")) != http.StatusInternalServerError {
		t.Error("plain error should map to 500")
	}
}
