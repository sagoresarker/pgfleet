package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/audit"
)

// fakeAuditLister is a test double for AuditLister. It records the limit it was
// called with and returns a canned result, so handler behaviour can be observed
// without a database.
type fakeAuditLister struct {
	entries   []audit.Entry
	err       error
	gotLimit  int
	callCount int
}

func (f *fakeAuditLister) List(_ context.Context, limit int) ([]audit.Entry, error) {
	f.callCount++
	f.gotLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

// auditResponse mirrors the JSON envelope the handler emits.
type auditResponse struct {
	Entries []struct {
		ID        string         `json:"id"`
		Actor     string         `json:"actor"`
		Action    string         `json:"action"`
		Target    string         `json:"target"`
		Metadata  map[string]any `json:"metadata"`
		CreatedAt string         `json:"created_at"`
	} `json:"entries"`
}

// TestAuditListReturnsEntriesAsJSON verifies the handler maps audit.Entry values
// into the stable JSON shape (id, actor, action, target, created_at in RFC3339).
func TestAuditListReturnsEntriesAsJSON(t *testing.T) {
	ts := time.Date(2026, 6, 3, 10, 30, 0, 0, time.UTC)
	lister := &fakeAuditLister{entries: []audit.Entry{
		{
			ID:        "a1",
			Actor:     "alice",
			Action:    "instance.create",
			Target:    "i-123",
			Metadata:  map[string]any{"region": "eu"},
			CreatedAt: ts,
		},
	}}
	h := NewAuditHandler(lister)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got auditResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, rec.Body.String())
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(got.Entries))
	}
	e := got.Entries[0]
	if e.ID != "a1" || e.Actor != "alice" || e.Action != "instance.create" || e.Target != "i-123" {
		t.Errorf("entry fields mismatched: %+v", e)
	}
	if e.CreatedAt != ts.Format(time.RFC3339) {
		t.Errorf("created_at = %q, want RFC3339 %q", e.CreatedAt, ts.Format(time.RFC3339))
	}
	if e.Metadata["region"] != "eu" {
		t.Errorf("metadata not carried through: %+v", e.Metadata)
	}
}

// TestAuditListDefaultLimit verifies that with no ?limit= the handler asks the
// store for the default page size.
func TestAuditListDefaultLimit(t *testing.T) {
	lister := &fakeAuditLister{}
	h := NewAuditHandler(lister)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	h.List(rec, req)

	if lister.gotLimit != auditDefaultLimit {
		t.Errorf("limit = %d, want default %d", lister.gotLimit, auditDefaultLimit)
	}
}

// TestAuditListRespectsLimit verifies a valid ?limit= is passed through.
func TestAuditListRespectsLimit(t *testing.T) {
	lister := &fakeAuditLister{}
	h := NewAuditHandler(lister)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit?limit=25", nil)
	h.List(rec, req)

	if lister.gotLimit != 25 {
		t.Errorf("limit = %d, want 25", lister.gotLimit)
	}
}

// TestAuditListClampsLimit verifies an over-large ?limit= is clamped to the max,
// preventing a client from forcing an unbounded scan.
func TestAuditListClampsLimit(t *testing.T) {
	lister := &fakeAuditLister{}
	h := NewAuditHandler(lister)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit?limit=100000", nil)
	h.List(rec, req)

	if lister.gotLimit != auditMaxLimit {
		t.Errorf("limit = %d, want clamped to max %d", lister.gotLimit, auditMaxLimit)
	}
}

// TestAuditListInvalidLimitFallsBackToDefault verifies a non-numeric or
// non-positive ?limit= falls back to the default rather than erroring.
func TestAuditListInvalidLimitFallsBackToDefault(t *testing.T) {
	for _, q := range []string{"limit=abc", "limit=0", "limit=-5"} {
		lister := &fakeAuditLister{}
		h := NewAuditHandler(lister)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/audit?"+q, nil)
		h.List(rec, req)

		if lister.gotLimit != auditDefaultLimit {
			t.Errorf("%s: limit = %d, want default %d", q, lister.gotLimit, auditDefaultLimit)
		}
	}
}

// TestAuditListEmptyIsArrayNotNull verifies an empty result serialises to
// {"entries": []} (a JSON array) and never {"entries": null}.
func TestAuditListEmptyIsArrayNotNull(t *testing.T) {
	lister := &fakeAuditLister{entries: nil}
	h := NewAuditHandler(lister)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// Must contain an empty array, never null.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("decode: %v; body=%s", err, body)
	}
	if string(raw["entries"]) != "[]" {
		t.Errorf("entries = %s, want []", raw["entries"])
	}
}

// TestAuditListSurfacesStoreError verifies a store failure becomes an HTTP error
// (not a 200 with an empty body).
func TestAuditListSurfacesStoreError(t *testing.T) {
	lister := &fakeAuditLister{err: context.DeadlineExceeded}
	h := NewAuditHandler(lister)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	h.List(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want an error status", rec.Code)
	}
}
