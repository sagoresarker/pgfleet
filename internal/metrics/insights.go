package metrics

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// QueryStat is one row of pg_stat_statements insight.
type QueryStat struct {
	Query       string  `json:"query"`
	Calls       int64   `json:"calls"`
	TotalTimeMS float64 `json:"total_time_ms"`
	MeanTimeMS  float64 `json:"mean_time_ms"`
	Rows        int64   `json:"rows"`
}

// TopQueries returns the most time-consuming statements from
// pg_stat_statements. If the extension is unavailable it returns an empty
// slice (best-effort), not an error.
func (c *Collector) TopQueries(ctx context.Context, dsn string, limit int) ([]QueryStat, error) {
	if limit <= 0 {
		limit = 20
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "metrics: connect", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT query, calls, total_exec_time, mean_exec_time, rows
		FROM pg_stat_statements
		ORDER BY total_exec_time DESC
		LIMIT $1`, limit)
	if err != nil {
		// Extension not installed / not preloaded: report no insights.
		return []QueryStat{}, nil
	}
	defer rows.Close()

	var out []QueryStat
	for rows.Next() {
		var q QueryStat
		if err := rows.Scan(&q.Query, &q.Calls, &q.TotalTimeMS, &q.MeanTimeMS, &q.Rows); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "metrics: scan query stat", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
