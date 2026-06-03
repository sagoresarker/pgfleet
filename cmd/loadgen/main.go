// Command loadgen is a load and consistency harness for a PgFleet-managed
// Postgres instance. It drives realistic write/update/delete/read traffic at a
// millions-of-rows scale and then proves the database stayed transactionally
// consistent under that concurrency.
//
// It exercises two workloads against one DSN:
//
//	accounts — a fixed pot of money spread over N accounts. Concurrent workers
//	           move money between random accounts inside a single transaction.
//	           No matter how many transfers run, SUM(balance) must equal the
//	           amount we seeded. That invariant is the consistency oracle: if it
//	           ever drifts, the platform lost an atomic write.
//
//	events   — an append-heavy ledger that grows into the millions, with a churn
//	           phase doing INSERT / UPDATE / DELETE / SELECT to stress autovacuum,
//	           indexes, bloat, and read/write contention.
//
// Phases (selectable with -mode): seed -> churn -> verify. "all" runs them in
// order. Seeding uses COPY for throughput; churn uses a worker pool for a fixed
// duration; verify re-reads the invariants and reports pass/fail with a non-zero
// exit code on any inconsistency, so it is CI-friendly.
//
// Usage:
//
//	loadgen -dsn "postgres://user:pass@host:5432/db?sslmode=disable" \
//	        -mode all -accounts 100000 -events 5000000 \
//	        -workers 32 -duration 2m
//
// The DSN can also come from $LOADGEN_DSN or $DATABASE_URL.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := parseFlags()
	if cfg.dsn == "" {
		fmt.Fprintln(os.Stderr, "error: a DSN is required (-dsn, $LOADGEN_DSN, or $DATABASE_URL)")
		os.Exit(2)
	}

	// Ctrl-C / SIGTERM cancels in-flight work cleanly so a long run can be
	// stopped without leaving half-open transactions.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := openPool(ctx, cfg)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	gen := &generator{cfg: cfg, pool: pool}

	if err := gen.run(ctx); err != nil {
		if errors.Is(err, errInconsistent) {
			log.Printf("CONSISTENCY CHECK FAILED: %v", err)
			os.Exit(1)
		}
		if errors.Is(err, context.Canceled) {
			log.Printf("interrupted; stopping")
			return
		}
		log.Fatalf("run: %v", err)
	}
}

// config holds every tunable for a run.
type config struct {
	dsn       string
	mode      string // seed | churn | verify | all
	accounts  int    // number of accounts in the consistency pot
	startBal  int64  // starting balance per account
	events    int    // target number of seeded event rows
	workers   int    // concurrent workers in the churn phase
	duration  time.Duration
	batch     int  // COPY/transfer batch size
	dropFirst bool // drop+recreate the schema before seeding
}

func parseFlags() config {
	var c config
	flag.StringVar(&c.dsn, "dsn", firstNonEmpty(os.Getenv("LOADGEN_DSN"), os.Getenv("DATABASE_URL")), "Postgres DSN")
	flag.StringVar(&c.mode, "mode", "all", "seed | churn | verify | all")
	flag.IntVar(&c.accounts, "accounts", 100_000, "number of accounts for the consistency invariant")
	flag.Int64Var(&c.startBal, "start-balance", 1_000, "starting balance per account")
	flag.IntVar(&c.events, "events", 5_000_000, "target number of seeded event rows")
	flag.IntVar(&c.workers, "workers", 32, "concurrent churn workers")
	flag.DurationVar(&c.duration, "duration", 2*time.Minute, "churn phase duration")
	flag.IntVar(&c.batch, "batch", 10_000, "rows per COPY batch / transfers per worker txn loop")
	flag.BoolVar(&c.dropFirst, "drop", false, "drop and recreate loadgen tables before seeding")
	flag.Parse()
	return c
}

func openPool(ctx context.Context, cfg config) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	// Give the pool enough connections for the worker fan-out plus headroom.
	pc.MaxConns = int32(cfg.workers + 4)
	pc.MaxConnLifetime = 30 * time.Minute
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(dialCtx, pc)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(dialCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

type generator struct {
	cfg  config
	pool *pgxpool.Pool
}

var errInconsistent = errors.New("invariant violated")

const ddl = `
CREATE TABLE IF NOT EXISTS loadgen_accounts (
	id      BIGINT PRIMARY KEY,
	balance BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS loadgen_events (
	id         BIGSERIAL PRIMARY KEY,
	account_id BIGINT NOT NULL,
	kind       TEXT   NOT NULL,
	amount     BIGINT NOT NULL,
	payload    JSONB  NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS loadgen_events_account_idx ON loadgen_events (account_id);
CREATE INDEX IF NOT EXISTS loadgen_events_created_idx ON loadgen_events (created_at);
`

func (g *generator) run(ctx context.Context) error {
	switch g.cfg.mode {
	case "seed":
		return g.seed(ctx)
	case "churn":
		return g.churn(ctx)
	case "verify":
		return g.verify(ctx)
	case "all":
		if err := g.seed(ctx); err != nil {
			return err
		}
		if err := g.churn(ctx); err != nil {
			return err
		}
		return g.verify(ctx)
	default:
		return fmt.Errorf("unknown mode %q (want seed|churn|verify|all)", g.cfg.mode)
	}
}

// seed creates the schema and bulk-loads accounts + events via COPY.
func (g *generator) seed(ctx context.Context) error {
	log.Printf("seed: ensuring schema (drop=%v)", g.cfg.dropFirst)
	if g.cfg.dropFirst {
		if _, err := g.pool.Exec(ctx, `DROP TABLE IF EXISTS loadgen_events, loadgen_accounts`); err != nil {
			return fmt.Errorf("drop: %w", err)
		}
	}
	if _, err := g.pool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("ddl: %w", err)
	}

	// Accounts: a known pot. Total = accounts * startBal, our consistency oracle.
	log.Printf("seed: loading %d accounts (start balance %d each)", g.cfg.accounts, g.cfg.startBal)
	if err := g.copyAccounts(ctx); err != nil {
		return err
	}
	total := int64(g.cfg.accounts) * g.cfg.startBal
	log.Printf("seed: seeded pot = %d", total)

	// Events: millions of append-only rows in COPY batches.
	log.Printf("seed: loading %d events in batches of %d", g.cfg.events, g.cfg.batch)
	start := time.Now()
	var loaded int64
	for loaded < int64(g.cfg.events) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n := int64(g.cfg.batch)
		if rem := int64(g.cfg.events) - loaded; rem < n {
			n = rem
		}
		if err := g.copyEvents(ctx, loaded, n); err != nil {
			return err
		}
		loaded += n
		if loaded%int64(g.cfg.batch*10) == 0 || loaded == int64(g.cfg.events) {
			rate := float64(loaded) / time.Since(start).Seconds()
			log.Printf("seed: %d/%d events (%.0f rows/s)", loaded, g.cfg.events, rate)
		}
	}
	log.Printf("seed: done in %s", time.Since(start).Round(time.Millisecond))
	return nil
}

func (g *generator) copyAccounts(ctx context.Context) error {
	id := int64(0)
	_, err := g.pool.CopyFrom(ctx,
		pgx.Identifier{"loadgen_accounts"},
		[]string{"id", "balance"},
		pgx.CopyFromFunc(func() ([]any, error) {
			if id >= int64(g.cfg.accounts) {
				return nil, nil
			}
			id++
			return []any{id, g.cfg.startBal}, nil
		}),
	)
	return err
}

// copyEvents loads n event rows whose ids start logically at offset (used only
// for deterministic-ish payloads; the PK is a serial).
func (g *generator) copyEvents(ctx context.Context, offset, n int64) error {
	kinds := []string{"deposit", "withdrawal", "transfer", "fee", "interest"}
	i := int64(0)
	_, err := g.pool.CopyFrom(ctx,
		pgx.Identifier{"loadgen_events"},
		[]string{"account_id", "kind", "amount", "payload", "created_at"},
		pgx.CopyFromFunc(func() ([]any, error) {
			if i >= n {
				return nil, nil
			}
			i++
			acct := rand.Int64N(int64(g.cfg.accounts)) + 1
			kind := kinds[rand.IntN(len(kinds))]
			amount := rand.Int64N(10_000)
			payload := fmt.Sprintf(`{"seq":%d,"src":"seed","note":"row-%d"}`, offset+i, offset+i)
			// Spread timestamps across the last ~30 days for realistic ranges.
			ts := time.Now().Add(-time.Duration(rand.Int64N(30*24)) * time.Hour)
			return []any{acct, kind, amount, payload, ts}, nil
		}),
	)
	return err
}

// churn runs a worker pool that mixes transactional transfers (the consistency
// workload) with event INSERT/UPDATE/DELETE/SELECT (the volume workload) for the
// configured duration.
func (g *generator) churn(ctx context.Context) error {
	if g.cfg.accounts < 2 {
		return errors.New("churn needs at least 2 accounts")
	}
	runCtx, cancel := context.WithTimeout(ctx, g.cfg.duration)
	defer cancel()

	var stats stats
	start := time.Now()
	log.Printf("churn: %d workers for %s", g.cfg.workers, g.cfg.duration)

	// Progress ticker.
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				stats.log(time.Since(start))
			}
		}
	}()

	var wg sync.WaitGroup
	for w := 0; w < g.cfg.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.worker(runCtx, &stats)
		}()
	}
	wg.Wait()
	close(done)

	stats.log(time.Since(start))
	log.Printf("churn: done in %s", time.Since(start).Round(time.Millisecond))
	if errors.Is(runCtx.Err(), context.Canceled) && ctx.Err() != nil {
		return ctx.Err() // external interrupt, not the duration timeout
	}
	return nil
}

// worker loops, picking a random operation, until the context is done. Each
// transactional failure (e.g. a serialization error) is retried implicitly on
// the next iteration; we never let a worker die on a transient error.
func (g *generator) worker(ctx context.Context, st *stats) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Weighted op mix: transfers and reads dominate, like a real workload.
		switch n := rand.IntN(100); {
		case n < 35:
			g.opTransfer(ctx, st)
		case n < 60:
			g.opInsertEvent(ctx, st)
		case n < 75:
			g.opUpdateEvent(ctx, st)
		case n < 85:
			g.opDeleteEvent(ctx, st)
		default:
			g.opSelect(ctx, st)
		}
	}
}

// opTransfer moves a random amount between two distinct accounts in one
// transaction and records an event for each side. This is the operation the
// consistency check is built around: it must be all-or-nothing.
func (g *generator) opTransfer(ctx context.Context, st *stats) {
	from := rand.Int64N(int64(g.cfg.accounts)) + 1
	to := rand.Int64N(int64(g.cfg.accounts)) + 1
	for to == from {
		to = rand.Int64N(int64(g.cfg.accounts)) + 1
	}
	amount := rand.Int64N(100) + 1

	err := pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		// Lock the lower id first to avoid deadlocks between mirrored transfers.
		a, b := from, to
		if a > b {
			a, b = b, a
		}
		var balA, balB int64
		if err := tx.QueryRow(ctx, `SELECT balance FROM loadgen_accounts WHERE id=$1 FOR UPDATE`, a).Scan(&balA); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT balance FROM loadgen_accounts WHERE id=$1 FOR UPDATE`, b).Scan(&balB); err != nil {
			return err
		}
		// Only move money that exists; never drive a balance negative. The pot
		// total is invariant regardless of whether any single transfer is skipped.
		if from == a {
			if balA < amount {
				return nil
			}
		} else if balB < amount {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE loadgen_accounts SET balance = balance - $1 WHERE id=$2`, amount, from); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE loadgen_accounts SET balance = balance + $1 WHERE id=$2`, amount, to); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO loadgen_events (account_id, kind, amount, payload)
			 VALUES ($1,'transfer',$2,$3), ($4,'transfer',$5,$6)`,
			from, -amount, fmt.Sprintf(`{"to":%d}`, to),
			to, amount, fmt.Sprintf(`{"from":%d}`, from),
		)
		return err
	})
	if err != nil {
		st.transferErr.Add(1)
		return
	}
	st.transfer.Add(1)
}

func (g *generator) opInsertEvent(ctx context.Context, st *stats) {
	acct := rand.Int64N(int64(g.cfg.accounts)) + 1
	_, err := g.pool.Exec(ctx,
		`INSERT INTO loadgen_events (account_id, kind, amount, payload)
		 VALUES ($1,'deposit',$2,$3)`,
		acct, rand.Int64N(1000), `{"src":"churn"}`)
	st.record(&st.insert, &st.insertErr, err)
}

func (g *generator) opUpdateEvent(ctx context.Context, st *stats) {
	acct := rand.Int64N(int64(g.cfg.accounts)) + 1
	_, err := g.pool.Exec(ctx,
		`UPDATE loadgen_events SET payload = payload || '{"touched":true}', amount = amount + 1
		 WHERE id IN (SELECT id FROM loadgen_events WHERE account_id=$1 ORDER BY id DESC LIMIT 5)`,
		acct)
	st.record(&st.update, &st.updateErr, err)
}

func (g *generator) opDeleteEvent(ctx context.Context, st *stats) {
	// Delete a small, old slice so the table churns without shrinking to nothing.
	_, err := g.pool.Exec(ctx,
		`DELETE FROM loadgen_events
		 WHERE id IN (SELECT id FROM loadgen_events WHERE kind='fee' ORDER BY id LIMIT 3)`)
	st.record(&st.del, &st.delErr, err)
}

func (g *generator) opSelect(ctx context.Context, st *stats) {
	acct := rand.Int64N(int64(g.cfg.accounts)) + 1
	var count int64
	var sum *int64
	err := g.pool.QueryRow(ctx,
		`SELECT count(*), sum(amount) FROM loadgen_events WHERE account_id=$1`, acct).Scan(&count, &sum)
	st.record(&st.read, &st.readErr, err)
}

// verify re-reads the consistency invariants and reports. It returns
// errInconsistent if the account pot drifted from the seeded total.
func (g *generator) verify(ctx context.Context) error {
	log.Printf("verify: checking invariants")

	var count int64
	var total *int64
	var minBal int64
	if err := g.pool.QueryRow(ctx,
		`SELECT count(*), sum(balance), coalesce(min(balance),0) FROM loadgen_accounts`).Scan(&count, &total, &minBal); err != nil {
		return fmt.Errorf("read accounts: %w", err)
	}
	if count == 0 {
		return errors.New("verify: no accounts found (seed first)")
	}
	got := int64(0)
	if total != nil {
		got = *total
	}
	want := count * g.cfg.startBal

	var events int64
	if err := g.pool.QueryRow(ctx, `SELECT count(*) FROM loadgen_events`).Scan(&events); err != nil {
		return fmt.Errorf("count events: %w", err)
	}

	// Orphan check: every event must reference a real account.
	var orphans int64
	if err := g.pool.QueryRow(ctx,
		`SELECT count(*) FROM loadgen_events e
		 LEFT JOIN loadgen_accounts a ON a.id = e.account_id
		 WHERE a.id IS NULL`).Scan(&orphans); err != nil {
		return fmt.Errorf("orphan check: %w", err)
	}

	log.Printf("verify: accounts=%d events=%d pot_want=%d pot_got=%d min_balance=%d orphans=%d",
		count, events, want, got, minBal, orphans)

	var problems []string
	if got != want {
		problems = append(problems, fmt.Sprintf("pot drifted by %d (want %d, got %d)", got-want, want, got))
	}
	if minBal < 0 {
		problems = append(problems, fmt.Sprintf("a balance went negative (min=%d)", minBal))
	}
	if orphans != 0 {
		problems = append(problems, fmt.Sprintf("%d orphan events", orphans))
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %v", errInconsistent, problems)
	}
	log.Printf("verify: PASS — pot conserved (%d), no negative balances, no orphans", got)
	return nil
}

// stats are atomic counters shared by the churn workers.
type stats struct {
	transfer, transferErr atomic.Int64
	insert, insertErr     atomic.Int64
	update, updateErr     atomic.Int64
	del, delErr           atomic.Int64
	read, readErr         atomic.Int64
}

func (s *stats) record(ok, fail *atomic.Int64, err error) {
	if err != nil {
		fail.Add(1)
		return
	}
	ok.Add(1)
}

func (s *stats) log(elapsed time.Duration) {
	totalOps := s.transfer.Load() + s.insert.Load() + s.update.Load() + s.del.Load() + s.read.Load()
	totalErr := s.transferErr.Load() + s.insertErr.Load() + s.updateErr.Load() + s.delErr.Load() + s.readErr.Load()
	rate := float64(totalOps) / elapsed.Seconds()
	log.Printf("churn: %.0fs ops=%d (%.0f/s) err=%d | xfer=%d ins=%d upd=%d del=%d read=%d",
		elapsed.Seconds(), totalOps, rate, totalErr,
		s.transfer.Load(), s.insert.Load(), s.update.Load(), s.del.Load(), s.read.Load())
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
