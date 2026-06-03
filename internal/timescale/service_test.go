package timescale

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// fakeConn is a hand-rolled Conn double recording the last SQL executed and
// returning canned results.
type fakeConn struct {
	lastExecSQL string
	execErr     error
}

func (f *fakeConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.lastExecSQL = sql
	return pgconn.CommandTag{}, f.execErr
}

func (f *fakeConn) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeConn) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}

// Compile-time assertion that *pgx.Conn satisfies the Conn interface.
var _ Conn = (*pgx.Conn)(nil)

func TestService_CreateHypertable_ExecutesBuiltSQL(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	if err := svc.CreateHypertable(context.Background(), fc, "metrics", "ts"); err != nil {
		t.Fatal(err)
	}
	want := "SELECT create_hypertable('metrics', by_range('ts'))"
	if fc.lastExecSQL != want {
		t.Errorf("exec sql = %q, want %q", fc.lastExecSQL, want)
	}
}

func TestService_CreateHypertable_ValidationError(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	err := svc.CreateHypertable(context.Background(), fc, "bad name", "ts")
	if err == nil {
		t.Fatal("want validation error")
	}
	if apperr.Kind(err) != apperr.KindInvalid {
		t.Errorf("kind = %v, want KindInvalid", apperr.Kind(err))
	}
	if fc.lastExecSQL != "" {
		t.Errorf("should not have executed; got %q", fc.lastExecSQL)
	}
}

func TestService_CreateHypertable_ExecErrorWrapped(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{execErr: errors.New("boom")}
	err := svc.CreateHypertable(context.Background(), fc, "metrics", "ts")
	if err == nil {
		t.Fatal("want exec error")
	}
	if apperr.Kind(err) != apperr.KindInternal {
		t.Errorf("kind = %v, want KindInternal", apperr.Kind(err))
	}
}

func TestService_AddRetentionPolicy(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	if err := svc.AddRetentionPolicy(context.Background(), fc, "metrics", "30 days"); err != nil {
		t.Fatal(err)
	}
	if fc.lastExecSQL != "SELECT add_retention_policy('metrics', INTERVAL '30 days')" {
		t.Errorf("unexpected sql %q", fc.lastExecSQL)
	}
}

func TestService_RemoveRetentionPolicy(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	if err := svc.RemoveRetentionPolicy(context.Background(), fc, "metrics"); err != nil {
		t.Fatal(err)
	}
	if fc.lastExecSQL != "SELECT remove_retention_policy('metrics')" {
		t.Errorf("unexpected sql %q", fc.lastExecSQL)
	}
}

func TestService_EnableCompression(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	if err := svc.EnableCompression(context.Background(), fc, "metrics", "device_id", "ts"); err != nil {
		t.Fatal(err)
	}
	want := "ALTER TABLE metrics SET (timescaledb.compress, timescaledb.compress_segmentby='device_id', timescaledb.compress_orderby='ts')"
	if fc.lastExecSQL != want {
		t.Errorf("unexpected sql %q", fc.lastExecSQL)
	}
}

func TestService_AddCompressionPolicy(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	if err := svc.AddCompressionPolicy(context.Background(), fc, "metrics", "7 days"); err != nil {
		t.Fatal(err)
	}
	if fc.lastExecSQL != "SELECT add_compression_policy('metrics', INTERVAL '7 days')" {
		t.Errorf("unexpected sql %q", fc.lastExecSQL)
	}
}

func TestService_RemoveCompressionPolicy(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	if err := svc.RemoveCompressionPolicy(context.Background(), fc, "metrics"); err != nil {
		t.Fatal(err)
	}
	if fc.lastExecSQL != "SELECT remove_compression_policy('metrics')" {
		t.Errorf("unexpected sql %q", fc.lastExecSQL)
	}
}

func TestService_CreateContinuousAggregate(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	q := "SELECT time_bucket('1 hour', ts) AS bucket, avg(value) FROM metrics GROUP BY bucket"
	if err := svc.CreateContinuousAggregate(context.Background(), fc, "metrics_hourly", q); err != nil {
		t.Fatal(err)
	}
	want := "CREATE MATERIALIZED VIEW metrics_hourly WITH (timescaledb.continuous) AS " + q + " WITH NO DATA"
	if fc.lastExecSQL != want {
		t.Errorf("unexpected sql %q", fc.lastExecSQL)
	}
}

func TestService_AddContinuousAggregatePolicy(t *testing.T) {
	svc := NewService()
	fc := &fakeConn{}
	if err := svc.AddContinuousAggregatePolicy(context.Background(), fc, "metrics_hourly", "1 month", "1 hour", "1 hour"); err != nil {
		t.Fatal(err)
	}
	want := "SELECT add_continuous_aggregate_policy('metrics_hourly', start_offset => INTERVAL '1 month', end_offset => INTERVAL '1 hour', schedule_interval => INTERVAL '1 hour')"
	if fc.lastExecSQL != want {
		t.Errorf("unexpected sql %q", fc.lastExecSQL)
	}
}
