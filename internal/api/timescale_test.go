package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/instance"
)

// fakeTSLookup is a stub tsInstanceLookup returning a fixed instance/error.
type fakeTSLookup struct {
	inst instance.Instance
	err  error
}

func (f fakeTSLookup) Get(context.Context, string) (instance.Instance, error) {
	return f.inst, f.err
}

// tsRequest builds a request whose chi route context carries URLParam "id",
// matching how the router invokes these handlers.
func tsRequest(method, id string) *http.Request {
	req := httptest.NewRequest(method, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestTimescaleConnectRequiresExtension verifies the guard returns 409 when the
// instance does not have timescaledb enabled, and never calls the DSN resolver.
func TestTimescaleConnectRequiresExtension(t *testing.T) {
	dsnCalled := false
	dsn := func(context.Context, string) (string, error) {
		dsnCalled = true
		return "", nil
	}
	lookup := fakeTSLookup{inst: instance.Instance{ID: "i1", Extensions: []string{"pgvector"}}}
	h := NewTimescaleHandler(lookup, dsn)

	rr := httptest.NewRecorder()
	h.List(rr, tsRequest(http.MethodGet, "i1"))

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
	if dsnCalled {
		t.Errorf("DSN resolver was called for an instance without timescaledb")
	}
}

// TestTimescaleConnectLookupError propagates the lookup error (e.g. not found)
// and does not call the DSN resolver.
func TestTimescaleConnectLookupError(t *testing.T) {
	dsnCalled := false
	dsn := func(context.Context, string) (string, error) {
		dsnCalled = true
		return "", nil
	}
	lookup := fakeTSLookup{err: errors.New("boom")}
	h := NewTimescaleHandler(lookup, dsn)

	rr := httptest.NewRecorder()
	h.Jobs(rr, tsRequest(http.MethodGet, "i1"))

	// A plain error maps to KindInternal -> 500.
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if dsnCalled {
		t.Errorf("DSN resolver was called despite lookup error")
	}
}

// TestTimescaleConnectDSNError verifies that a DSN-resolution failure for an
// otherwise-eligible instance surfaces as a 500 (KindInternal).
func TestTimescaleConnectDSNError(t *testing.T) {
	dsn := func(context.Context, string) (string, error) {
		return "", errors.New("vault unavailable")
	}
	lookup := fakeTSLookup{inst: instance.Instance{ID: "i1", Extensions: []string{"timescaledb"}}}
	h := NewTimescaleHandler(lookup, dsn)

	rr := httptest.NewRecorder()
	h.List(rr, tsRequest(http.MethodGet, "i1"))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}
