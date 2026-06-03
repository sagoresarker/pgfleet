package pgcat

import "testing"

func TestParsePoolRows(t *testing.T) {
	rows := []map[string]string{
		{
			"database": "postgres", "user": "postgres",
			"pool_mode": "transaction",
			"cl_active": "3", "cl_waiting": "1", "cl_idle": "2",
			"sv_active": "4", "sv_idle": "6", "sv_used": "1", "sv_tested": "0", "sv_login": "0",
			"maxwait": "0", "maxwait_us": "1500",
		},
	}
	pools := parsePoolRows(rows)
	if len(pools) != 1 {
		t.Fatalf("len = %d, want 1", len(pools))
	}
	p := pools[0]
	if p.Database != "postgres" || p.User != "postgres" || p.PoolMode != "transaction" {
		t.Errorf("identity = %+v", p)
	}
	if p.ClActive != 3 || p.ClWaiting != 1 || p.ClIdle != 2 {
		t.Errorf("client counts = %+v", p)
	}
	if p.SvActive != 4 || p.SvIdle != 6 || p.SvUsed != 1 {
		t.Errorf("server counts = %+v", p)
	}
	if p.Maxwait != 0 || p.MaxwaitUs != 1500 {
		t.Errorf("maxwait = %+v", p)
	}
}

func TestParsePoolRowsToleratesMissingAndBadFields(t *testing.T) {
	rows := []map[string]string{
		{"database": "db1", "cl_waiting": "not-a-number"},
	}
	pools := parsePoolRows(rows)
	if len(pools) != 1 || pools[0].Database != "db1" {
		t.Fatalf("pools = %+v", pools)
	}
	if pools[0].ClWaiting != 0 {
		t.Errorf("bad numeric should default to 0, got %d", pools[0].ClWaiting)
	}
}

func TestParseServerRows(t *testing.T) {
	rows := []map[string]string{
		{
			"server_id": "0xab", "database_name": "postgres", "user": "postgres",
			"address_id": "pgfleet-pg-orders-p:5432", "application_name": "pgcat",
			"state": "active", "transaction_count": "12", "query_count": "40",
			"bytes_sent": "2048", "bytes_received": "4096", "age_seconds": "30",
			"prepare_cache_hit": "5", "prepare_cache_miss": "1",
		},
	}
	servers := parseServerRows(rows)
	if len(servers) != 1 {
		t.Fatalf("len = %d, want 1", len(servers))
	}
	s := servers[0]
	if s.Address != "pgfleet-pg-orders-p:5432" || s.Database != "postgres" || s.State != "active" {
		t.Errorf("identity = %+v", s)
	}
	if s.QueryCount != 40 || s.TransactionCount != 12 {
		t.Errorf("counts = %+v", s)
	}
	if s.BytesSent != 2048 || s.BytesReceived != 4096 {
		t.Errorf("bytes = %+v", s)
	}
	if s.AgeSeconds != 30 {
		t.Errorf("age = %d", s.AgeSeconds)
	}
}

func TestParseServerRowsToleratesMissingFields(t *testing.T) {
	rows := []map[string]string{{"address_id": "host", "query_count": "oops"}}
	servers := parseServerRows(rows)
	if len(servers) != 1 || servers[0].Address != "host" || servers[0].QueryCount != 0 {
		t.Fatalf("servers = %+v", servers)
	}
}

func TestParseClientRows(t *testing.T) {
	rows := []map[string]string{
		{
			"client_id": "0xcd", "database": "postgres", "user": "app",
			"application_name": "web", "state": "active",
			"transaction_count": "7", "query_count": "21", "error_count": "1",
			"age_seconds": "12", "maxwait": "0", "maxwait_us": "900",
		},
	}
	clients := parseClientRows(rows)
	if len(clients) != 1 {
		t.Fatalf("len = %d, want 1", len(clients))
	}
	c := clients[0]
	if c.Database != "postgres" || c.User != "app" || c.ApplicationName != "web" || c.State != "active" {
		t.Errorf("identity = %+v", c)
	}
	if c.QueryCount != 21 || c.TransactionCount != 7 || c.ErrorCount != 1 {
		t.Errorf("counts = %+v", c)
	}
	if c.AgeSeconds != 12 || c.MaxwaitUs != 900 {
		t.Errorf("timing = %+v", c)
	}
}

func TestParseStatRows(t *testing.T) {
	rows := []map[string]string{
		{
			"database":          "postgres",
			"total_xact_count":  "100",
			"total_query_count": "250",
			"total_received":    "4096",
			"total_sent":        "8192",
			"total_xact_time":   "5000",
			"total_query_time":  "3000",
			"total_wait_time":   "120",
			"avg_xact_count":    "2",
			"avg_query_count":   "5",
			"avg_xact_time":     "50",
			"avg_query_time":    "12",
			"avg_wait_time":     "1",
		},
	}
	stats := parseStatRows(rows)
	if len(stats) != 1 {
		t.Fatalf("len = %d, want 1", len(stats))
	}
	s := stats[0]
	if s.Database != "postgres" {
		t.Errorf("database = %q", s.Database)
	}
	if s.TotalXactCount != 100 || s.TotalQueryCount != 250 {
		t.Errorf("totals = %+v", s)
	}
	if s.AvgQueryTime != 12 || s.AvgXactTime != 50 {
		t.Errorf("avg times = %+v", s)
	}
}
