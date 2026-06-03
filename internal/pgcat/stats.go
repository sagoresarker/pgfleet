package pgcat

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// PoolStat is one row of PgCat's SHOW POOLS: a (database, user) pool with its
// client/server connection counts and the longest a client has waited for a
// server connection (maxwait).
type PoolStat struct {
	Database  string `json:"database"`
	User      string `json:"user"`
	PoolMode  string `json:"pool_mode"`
	ClActive  int64  `json:"cl_active"`
	ClWaiting int64  `json:"cl_waiting"`
	ClIdle    int64  `json:"cl_idle"`
	SvActive  int64  `json:"sv_active"`
	SvIdle    int64  `json:"sv_idle"`
	SvUsed    int64  `json:"sv_used"`
	SvTested  int64  `json:"sv_tested"`
	SvLogin   int64  `json:"sv_login"`
	Maxwait   int64  `json:"maxwait"`
	MaxwaitUs int64  `json:"maxwait_us"`
}

// Stat is one row of PgCat's SHOW STATS: cumulative and per-second-averaged
// transaction/query counters and timings for a database.
type Stat struct {
	Database        string `json:"database"`
	TotalXactCount  int64  `json:"total_xact_count"`
	TotalQueryCount int64  `json:"total_query_count"`
	TotalReceived   int64  `json:"total_received"`
	TotalSent       int64  `json:"total_sent"`
	TotalXactTime   int64  `json:"total_xact_time"`
	TotalQueryTime  int64  `json:"total_query_time"`
	TotalWaitTime   int64  `json:"total_wait_time"`
	AvgXactCount    int64  `json:"avg_xact_count"`
	AvgQueryCount   int64  `json:"avg_query_count"`
	AvgXactTime     int64  `json:"avg_xact_time"`
	AvgQueryTime    int64  `json:"avg_query_time"`
	AvgWaitTime     int64  `json:"avg_wait_time"`
}

// ServerStat is one row of PgCat's SHOW SERVERS: a single backend (server)
// connection the router holds. Address is PgCat's address_name (the configured
// backend host) which the caller maps back to a cluster member to attribute
// traffic to the primary vs a replica. The counters are cumulative for the
// lifetime of that connection (they reset when the connection recycles), so
// they read as an instantaneous load share, not a monotonic rate.
type ServerStat struct {
	ServerID         string `json:"server_id"`
	Database         string `json:"database"`
	User             string `json:"user"`
	Address          string `json:"address"`
	ApplicationName  string `json:"application_name"`
	State            string `json:"state"`
	TransactionCount int64  `json:"transaction_count"`
	QueryCount       int64  `json:"query_count"`
	BytesSent        int64  `json:"bytes_sent"`
	BytesReceived    int64  `json:"bytes_received"`
	AgeSeconds       int64  `json:"age_seconds"`
	PrepareCacheHit  int64  `json:"prepare_cache_hit"`
	PrepareCacheMiss int64  `json:"prepare_cache_miss"`
}

// ClientStat is one row of PgCat's SHOW CLIENTS: a single client (application)
// connection in front of the router, with its lifetime transaction/query/error
// counts and how long it has waited for a server.
type ClientStat struct {
	ClientID         string `json:"client_id"`
	Database         string `json:"database"`
	User             string `json:"user"`
	ApplicationName  string `json:"application_name"`
	State            string `json:"state"`
	TransactionCount int64  `json:"transaction_count"`
	QueryCount       int64  `json:"query_count"`
	ErrorCount       int64  `json:"error_count"`
	AgeSeconds       int64  `json:"age_seconds"`
	Maxwait          int64  `json:"maxwait"`
	MaxwaitUs        int64  `json:"maxwait_us"`
}

// PoolStats is the parsed result of the router admin SHOW commands. Pools and
// Stats come from SHOW POOLS / SHOW STATS; Servers and Clients (SHOW SERVERS /
// SHOW CLIENTS) are best-effort — an older PgCat that lacks them yields empty
// slices rather than failing the whole read.
type PoolStats struct {
	Pools   []PoolStat   `json:"pools"`
	Stats   []Stat       `json:"stats"`
	Servers []ServerStat `json:"servers"`
	Clients []ClientStat `json:"clients"`
}

// ReadPoolStats connects to a PgCat ADMIN interface (the router's host:port as
// the admin user, database "pgcat") and returns the parsed SHOW POOLS / SHOW
// STATS output. adminDSN is a standard postgres DSN whose user/password are the
// router's admin credentials and whose dbname is "pgcat".
func ReadPoolStats(ctx context.Context, adminDSN string) (PoolStats, error) {
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return PoolStats{}, err
	}
	defer conn.Close(ctx)

	poolRows, err := queryAdmin(ctx, conn, "SHOW POOLS")
	if err != nil {
		return PoolStats{}, err
	}
	statRows, err := queryAdmin(ctx, conn, "SHOW STATS")
	if err != nil {
		return PoolStats{}, err
	}

	// SHOW SERVERS / SHOW CLIENTS are best-effort: they drive the routing view
	// and connection tables but must not break the core pool/stats panel if a
	// PgCat build doesn't support them.
	var servers []ServerStat
	if serverRows, err := queryAdmin(ctx, conn, "SHOW SERVERS"); err == nil {
		servers = parseServerRows(serverRows)
	}
	var clients []ClientStat
	if clientRows, err := queryAdmin(ctx, conn, "SHOW CLIENTS"); err == nil {
		clients = parseClientRows(clientRows)
	}

	return PoolStats{
		Pools:   parsePoolRows(poolRows),
		Stats:   parseStatRows(statRows),
		Servers: servers,
		Clients: clients,
	}, nil
}

// queryAdmin runs a SHOW command and returns each result row as a column-name
// keyed map of string values, decoupling SQL access from parsing so the
// parsers are unit-testable without a live PgCat.
func queryAdmin(ctx context.Context, conn *pgx.Conn, sql string) ([]map[string]string, error) {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := rows.FieldDescriptions()
	var out []map[string]string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]string, len(cols))
		for i, c := range cols {
			if i < len(vals) {
				m[c.Name] = stringify(vals[i])
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func parsePoolRows(rows []map[string]string) []PoolStat {
	out := make([]PoolStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, PoolStat{
			Database:  r["database"],
			User:      r["user"],
			PoolMode:  r["pool_mode"],
			ClActive:  atoi(r["cl_active"]),
			ClWaiting: atoi(r["cl_waiting"]),
			ClIdle:    atoi(r["cl_idle"]),
			SvActive:  atoi(r["sv_active"]),
			SvIdle:    atoi(r["sv_idle"]),
			SvUsed:    atoi(r["sv_used"]),
			SvTested:  atoi(r["sv_tested"]),
			SvLogin:   atoi(r["sv_login"]),
			Maxwait:   atoi(r["maxwait"]),
			MaxwaitUs: atoi(r["maxwait_us"]),
		})
	}
	return out
}

func parseStatRows(rows []map[string]string) []Stat {
	out := make([]Stat, 0, len(rows))
	for _, r := range rows {
		out = append(out, Stat{
			Database:        r["database"],
			TotalXactCount:  atoi(r["total_xact_count"]),
			TotalQueryCount: atoi(r["total_query_count"]),
			TotalReceived:   atoi(r["total_received"]),
			TotalSent:       atoi(r["total_sent"]),
			TotalXactTime:   atoi(r["total_xact_time"]),
			TotalQueryTime:  atoi(r["total_query_time"]),
			TotalWaitTime:   atoi(r["total_wait_time"]),
			AvgXactCount:    atoi(r["avg_xact_count"]),
			AvgQueryCount:   atoi(r["avg_query_count"]),
			AvgXactTime:     atoi(r["avg_xact_time"]),
			AvgQueryTime:    atoi(r["avg_query_time"]),
			AvgWaitTime:     atoi(r["avg_wait_time"]),
		})
	}
	return out
}

func parseServerRows(rows []map[string]string) []ServerStat {
	out := make([]ServerStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, ServerStat{
			ServerID:         r["server_id"],
			Database:         r["database_name"],
			User:             r["user"],
			Address:          r["address_id"],
			ApplicationName:  r["application_name"],
			State:            r["state"],
			TransactionCount: atoi(r["transaction_count"]),
			QueryCount:       atoi(r["query_count"]),
			BytesSent:        atoi(r["bytes_sent"]),
			BytesReceived:    atoi(r["bytes_received"]),
			AgeSeconds:       atoi(r["age_seconds"]),
			PrepareCacheHit:  atoi(r["prepare_cache_hit"]),
			PrepareCacheMiss: atoi(r["prepare_cache_miss"]),
		})
	}
	return out
}

func parseClientRows(rows []map[string]string) []ClientStat {
	out := make([]ClientStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, ClientStat{
			ClientID:         r["client_id"],
			Database:         r["database"],
			User:             r["user"],
			ApplicationName:  r["application_name"],
			State:            r["state"],
			TransactionCount: atoi(r["transaction_count"]),
			QueryCount:       atoi(r["query_count"]),
			ErrorCount:       atoi(r["error_count"]),
			AgeSeconds:       atoi(r["age_seconds"]),
			Maxwait:          atoi(r["maxwait"]),
			MaxwaitUs:        atoi(r["maxwait_us"]),
		})
	}
	return out
}

// atoi parses a PgCat admin counter, returning 0 for empty or malformed values
// (the admin protocol returns plain integers, but be defensive).
func atoi(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
