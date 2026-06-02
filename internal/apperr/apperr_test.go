package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestErrorMessage(t *testing.T) {
	err := New(KindNotFound, "instance not found")
	if err.Error() != "instance not found" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestKindReportsKind(t *testing.T) {
	err := New(KindConflict, "already exists")
	if Kind(err) != KindConflict {
		t.Errorf("Kind() = %v, want %v", Kind(err), KindConflict)
	}
}

func TestKindOfPlainErrorIsInternal(t *testing.T) {
	if Kind(errors.New("boom")) != KindInternal {
		t.Errorf("plain error Kind() = %v, want KindInternal", Kind(errors.New("boom")))
	}
	if Kind(nil) != KindInternal {
		t.Errorf("nil Kind() = %v, want KindInternal", Kind(nil))
	}
}

func TestHTTPStatusMapping(t *testing.T) {
	cases := []struct {
		kind Code
		want int
	}{
		{KindInvalid, http.StatusBadRequest},
		{KindUnauthorized, http.StatusUnauthorized},
		{KindForbidden, http.StatusForbidden},
		{KindNotFound, http.StatusNotFound},
		{KindConflict, http.StatusConflict},
		{KindInternal, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		err := New(tc.kind, "x")
		if got := HTTPStatus(err); got != tc.want {
			t.Errorf("HTTPStatus(%v) = %d, want %d", tc.kind, got, tc.want)
		}
	}
}

func TestHTTPStatusOfPlainErrorIs500(t *testing.T) {
	if got := HTTPStatus(errors.New("boom")); got != http.StatusInternalServerError {
		t.Errorf("HTTPStatus(plain) = %d, want 500", got)
	}
}

func TestWrapPreservesKindAndUnwraps(t *testing.T) {
	base := errors.New("pq: duplicate key")
	err := Wrap(KindConflict, "creating user", base)

	if Kind(err) != KindConflict {
		t.Errorf("Kind() = %v, want KindConflict", Kind(err))
	}
	if !errors.Is(err, base) {
		t.Error("Wrap should preserve the wrapped error for errors.Is")
	}
}

func TestKindFindsKindThroughWrappedChain(t *testing.T) {
	err := fmt.Errorf("outer: %w", New(KindForbidden, "nope"))
	if Kind(err) != KindForbidden {
		t.Errorf("Kind() through fmt.Errorf chain = %v, want KindForbidden", Kind(err))
	}
}
