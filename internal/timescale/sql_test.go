package timescale

import "testing"

func TestCreateHypertableSQL(t *testing.T) {
	got, err := CreateHypertableSQL("metrics", "ts")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT create_hypertable('metrics', by_range('ts'))"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestCreateHypertableSQL_Qualified(t *testing.T) {
	got, err := CreateHypertableSQL("public.metrics", "ts")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT create_hypertable('public.metrics', by_range('ts'))"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestCreateHypertableSQL_Injection(t *testing.T) {
	cases := [][2]string{
		{"metrics'); DROP TABLE x;--", "ts"},
		{"metrics", "ts'); DROP TABLE x;--"},
		{"", "ts"},
		{"metrics", ""},
		{"a b", "ts"},
		{"a.b.c", "ts"},
	}
	for _, c := range cases {
		if _, err := CreateHypertableSQL(c[0], c[1]); err == nil {
			t.Errorf("CreateHypertableSQL(%q,%q) = nil err, want error", c[0], c[1])
		}
	}
}

func TestAddRetentionPolicySQL(t *testing.T) {
	got, err := AddRetentionPolicySQL("metrics", "30 days")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT add_retention_policy('metrics', INTERVAL '30 days')"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestAddRetentionPolicySQL_Injection(t *testing.T) {
	cases := [][2]string{
		{"metrics", "30 days'); DROP TABLE x;--"},
		{"metrics'); x", "30 days"},
		{"metrics", ""},
		{"metrics", "30 days)"},
	}
	for _, c := range cases {
		if _, err := AddRetentionPolicySQL(c[0], c[1]); err == nil {
			t.Errorf("AddRetentionPolicySQL(%q,%q) = nil err, want error", c[0], c[1])
		}
	}
}

func TestRemoveRetentionPolicySQL(t *testing.T) {
	got, err := RemoveRetentionPolicySQL("metrics")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT remove_retention_policy('metrics')"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if _, err := RemoveRetentionPolicySQL("a;b"); err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestEnableCompressionSQL_Full(t *testing.T) {
	got, err := EnableCompressionSQL("metrics", "device_id", "ts")
	if err != nil {
		t.Fatal(err)
	}
	want := "ALTER TABLE metrics SET (timescaledb.compress, timescaledb.compress_segmentby='device_id', timescaledb.compress_orderby='ts')"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestEnableCompressionSQL_OnlyCompress(t *testing.T) {
	got, err := EnableCompressionSQL("metrics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "ALTER TABLE metrics SET (timescaledb.compress)"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestEnableCompressionSQL_SegmentOnly(t *testing.T) {
	got, err := EnableCompressionSQL("metrics", "device_id, region", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "ALTER TABLE metrics SET (timescaledb.compress, timescaledb.compress_segmentby='device_id, region')"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestEnableCompressionSQL_OrderOnly(t *testing.T) {
	got, err := EnableCompressionSQL("metrics", "", "ts")
	if err != nil {
		t.Fatal(err)
	}
	want := "ALTER TABLE metrics SET (timescaledb.compress, timescaledb.compress_orderby='ts')"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestEnableCompressionSQL_Injection(t *testing.T) {
	cases := [][3]string{
		{"metrics'); x", "", ""},
		{"metrics", "device_id'); x", ""},
		{"metrics", "", "ts'); x"},
		{"metrics", "a b", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if _, err := EnableCompressionSQL(c[0], c[1], c[2]); err == nil {
			t.Errorf("EnableCompressionSQL(%q,%q,%q) = nil err, want error", c[0], c[1], c[2])
		}
	}
}

func TestAddCompressionPolicySQL(t *testing.T) {
	got, err := AddCompressionPolicySQL("metrics", "7 days")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT add_compression_policy('metrics', INTERVAL '7 days')"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if _, err := AddCompressionPolicySQL("metrics", "7 days'); x"); err == nil {
		t.Error("expected error for injection interval")
	}
}

func TestRemoveCompressionPolicySQL(t *testing.T) {
	got, err := RemoveCompressionPolicySQL("metrics")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT remove_compression_policy('metrics')"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestCreateContinuousAggregateSQL(t *testing.T) {
	q := "SELECT time_bucket('1 hour', ts) AS bucket, avg(value) FROM metrics GROUP BY bucket"
	got, err := CreateContinuousAggregateSQL("metrics_hourly", q)
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE MATERIALIZED VIEW metrics_hourly WITH (timescaledb.continuous) AS " + q + " WITH NO DATA"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestCreateContinuousAggregateSQL_Rejects(t *testing.T) {
	cases := [][2]string{
		{"bad name", "SELECT 1"},
		{"agg", "SELECT 1; DROP TABLE x"}, // statement chaining via query
		{"agg", ""},                       // empty query
		{"a;b", "SELECT 1"},
	}
	for _, c := range cases {
		if _, err := CreateContinuousAggregateSQL(c[0], c[1]); err == nil {
			t.Errorf("CreateContinuousAggregateSQL(%q,%q) = nil err, want error", c[0], c[1])
		}
	}
}

func TestAddContinuousAggregatePolicySQL(t *testing.T) {
	got, err := AddContinuousAggregatePolicySQL("metrics_hourly", "1 month", "1 hour", "1 hour")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT add_continuous_aggregate_policy('metrics_hourly', start_offset => INTERVAL '1 month', end_offset => INTERVAL '1 hour', schedule_interval => INTERVAL '1 hour')"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestAddContinuousAggregatePolicySQL_Rejects(t *testing.T) {
	cases := [][4]string{
		{"a;b", "1 month", "1 hour", "1 hour"},
		{"agg", "1 month'); x", "1 hour", "1 hour"},
		{"agg", "1 month", "1 hour)", "1 hour"},
		{"agg", "1 month", "1 hour", ""},
	}
	for _, c := range cases {
		if _, err := AddContinuousAggregatePolicySQL(c[0], c[1], c[2], c[3]); err == nil {
			t.Errorf("AddContinuousAggregatePolicySQL(%v) = nil err, want error", c)
		}
	}
}
