package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeSQLRows is an in-memory implementation of the sqlRows interface used to
// unit-test the row-collection / byte-budget logic without a real database.
type fakeSQLRows struct {
	cols   []string
	data   [][]any
	idx    int
	err    error
	closed bool
}

func (f *fakeSQLRows) FieldDescriptions() []pgconn.FieldDescription {
	fds := make([]pgconn.FieldDescription, 0, len(f.cols))
	for _, c := range f.cols {
		fds = append(fds, pgconn.FieldDescription{Name: c})
	}
	return fds
}

func (f *fakeSQLRows) Next() bool {
	if f.idx >= len(f.data) {
		return false
	}
	f.idx++
	return true
}

func (f *fakeSQLRows) Values() ([]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.data[f.idx-1], nil
}

func (f *fakeSQLRows) Err() error { return f.err }
func (f *fakeSQLRows) Close()     { f.closed = true }

// TestSQLConnectErrorIsInternal locks in SEC-2: a transport/connect failure is
// OUR problem and must map to 500 (KindInternal), not a 400 user error. We use
// a DSN whose port is closed so pgx.Connect fails.
func TestSQLConnectErrorIsInternal(t *testing.T) {
	dsn := func(context.Context, string) (string, error) {
		// 127.0.0.1:1 is a reserved/closed port -> connection refused.
		return "postgres://u:p@127.0.0.1:1/x?connect_timeout=1", nil
	}
	h := NewSQLHandler(dsn)
	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"select 1"}`)), "i1")
	h.Run(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("connect failure status = %d, want 500 (KindInternal)", rr.Code)
	}
}

// TestSQLEmptyQueryIsInvalid confirms an empty query is a 400 user error and
// never reaches the DSN resolver / connection.
func TestSQLEmptyQueryIsInvalid(t *testing.T) {
	called := false
	dsn := func(context.Context, string) (string, error) {
		called = true
		return "", nil
	}
	h := NewSQLHandler(dsn)
	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":""}`)), "i1")
	h.Run(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty query status = %d, want 400", rr.Code)
	}
	if called {
		t.Fatalf("DSN resolver called for an empty query")
	}
}

// TestCollectRowsByteBudget verifies that a result whose total cell bytes exceed
// the byte budget is truncated even when the row count is far below maxSQLRows.
// This is the SEC-4 regression: a single huge row (or a few) must not be fully
// buffered into memory.
func TestCollectRowsByteBudget(t *testing.T) {
	// Two rows of ~5MB each -> 10MB total, well over the ~8MB budget, but only
	// 2 rows so the row-count cap (1000) would NOT catch it.
	big := strings.Repeat("x", 5<<20)
	rows := &fakeSQLRows{
		cols: []string{"data"},
		data: [][]any{{big}, {big}, {big}},
	}

	cols, out, truncated, err := collectRows(rows)
	if err != nil {
		t.Fatalf("collectRows error: %v", err)
	}
	if len(cols) != 1 || cols[0] != "data" {
		t.Fatalf("cols = %v, want [data]", cols)
	}
	if !truncated {
		t.Fatalf("truncated = false, want true (byte budget should have tripped)")
	}
	// Must have stopped before buffering all 3 rows (3*5MB = 15MB).
	if len(out) >= 3 {
		t.Fatalf("collected %d rows; byte budget should have stopped earlier", len(out))
	}
}

// TestCollectRowsRowCountCap verifies the existing row-count cap still applies
// for many small rows (below the byte budget).
func TestCollectRowsRowCountCap(t *testing.T) {
	data := make([][]any, maxSQLRows+50)
	for i := range data {
		data[i] = []any{int64(i)}
	}
	rows := &fakeSQLRows{cols: []string{"n"}, data: data}

	_, out, truncated, err := collectRows(rows)
	if err != nil {
		t.Fatalf("collectRows error: %v", err)
	}
	if !truncated {
		t.Fatalf("truncated = false, want true (row-count cap)")
	}
	if len(out) != maxSQLRows {
		t.Fatalf("collected %d rows, want %d (row-count cap)", len(out), maxSQLRows)
	}
}

// TestCollectRowsSmallResult verifies a normal small result is returned whole
// and not flagged truncated.
func TestCollectRowsSmallResult(t *testing.T) {
	rows := &fakeSQLRows{
		cols: []string{"a", "b"},
		data: [][]any{{int64(1), "x"}, {int64(2), "y"}},
	}
	_, out, truncated, err := collectRows(rows)
	if err != nil {
		t.Fatalf("collectRows error: %v", err)
	}
	if truncated {
		t.Fatalf("truncated = true, want false for a small result")
	}
	if len(out) != 2 {
		t.Fatalf("collected %d rows, want 2", len(out))
	}
}

// TestCollectRowsValuesError surfaces a per-row read error.
func TestCollectRowsValuesError(t *testing.T) {
	rows := &fakeSQLRows{
		cols: []string{"a"},
		data: [][]any{{int64(1)}},
		err:  errors.New("decode boom"),
	}
	_, _, _, err := collectRows(rows)
	if err == nil {
		t.Fatalf("expected an error from a failing Values()")
	}
}

// TestCollectRowsCountsByteValues verifies []byte cell values count toward the
// budget (not just strings), so a binary blob result is also bounded.
func TestCollectRowsCountsByteValues(t *testing.T) {
	blob := make([]byte, 5<<20)
	rows := &fakeSQLRows{
		cols: []string{"blob"},
		data: [][]any{{blob}, {blob}, {blob}},
	}
	_, out, truncated, err := collectRows(rows)
	if err != nil {
		t.Fatalf("collectRows error: %v", err)
	}
	if !truncated {
		t.Fatalf("truncated = false, want true for oversized []byte result")
	}
	if len(out) >= 3 {
		t.Fatalf("collected %d rows; byte budget should have stopped before buffering all", len(out))
	}
}
