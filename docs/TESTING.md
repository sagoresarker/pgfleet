# Testing & Coverage

PgFleet is built test-first (TDD, red → green → refactor). Tests run in two
tiers so the inner loop stays fast while real infrastructure behaviour is still
exercised.

## The two tiers

| Tier | Tag | What it uses | When it runs |
|------|-----|--------------|--------------|
| **Unit** | _(none)_ | Pure logic + in-memory fakes (no Docker, no DB) | every `go test ./...`, sub-second |
| **Integration** | `//go:build integration` | Real Postgres + MinIO + pgBackRest + PgCat via [testcontainers-go](https://golang.testcontainers.org/) | `go test -tags=integration ./...`, Docker required |

The split is deliberate: configuration generation, command building,
`pgbackrest info` parsing, RBAC, JWT, the secrets cipher, the scheduler, the
reconciler, and the WebSocket hub are **pure unit tests**. Anything that touches
a database, a container, or a real Postgres cluster is an **integration test**
behind the build tag, so the unit suite never needs Docker.

## How to run

```bash
make test               # unit suite, race detector, sub-second
make test-integration   # full integration suite (Docker Desktop required)
go test -race ./...      # what CI's unit job runs
go test -race -tags=integration -timeout 30m ./...   # CI's integration job

# frontend
cd web && npm test       # Vitest component/util tests
cd web && npm run build  # type-check + production build
```

Integration tests use Docker Desktop with no extra configuration. They spin
throwaway Postgres/MinIO containers and clean them up via the testcontainers
Ryuk reaper, so they're safe to run repeatedly.

## Coverage

Authoritative **combined (unit + integration) coverage is 78.3%** of statements
across all packages, measured with cross-package attribution:

```bash
go test -tags=integration -coverprofile=cov.out -coverpkg=./internal/... ./internal/...
go tool cover -func=cov.out | tail -1     # total
go tool cover -html=cov.out               # line-by-line in a browser
```

Per-package combined coverage:

| Package | Coverage | Package | Coverage |
|---------|---------:|---------|---------:|
| `version`, `logging`, `apperr` | 100% | `health` | 89% |
| `pgbackrest` | 100% | `metrics` | 86% |
| `pgcat` | 100% | `bootstrap` | 85% |
| `store` | 98% | `docker` | 84% |
| `ws` | 98% | `cluster` | 84% |
| `scheduler` | 96% | `objectstore` | 78% |
| `user`, `auth` | 95% | `backup` | 77% |
| `config` | 94% | `provision` | 73% |
| `reconcile`, `audit` | 91% | `api` | 72% |
| `instance` | 90% | `clusterctl` | 69% |
| `secrets` | 89% | | |

Pure-logic packages are at or near 100%. The lower numbers (`provision`, `api`,
`clusterctl`) are the orchestration layers, where the uncovered statements are
mostly Docker-error and partial-failure branches that are hard to force
deterministically; the happy paths and the important failure paths (resource
cleanup, slot drop, restore rollback) are covered by integration tests.

### Packages added since the last full combined run

These shipped after the headline figure above was measured. The numbers here are
**unit-only** (no `-tags=integration`); the combined figure is higher because the
real logic in `metabackup`, `events`, `alerts`, and `objectstore` is exercised by
Docker/MinIO/real-Postgres integration tests, not the unit suite. Re-run the
combined command above to refresh the headline.

| Package | Unit cov. | What it does | Where the rest is proven |
|---------|----------:|--------------|--------------------------|
| `pgconfig` | 100% | platform-owned GUC merge + tuning | pure logic, fully unit-covered |
| `failover` | 79% | in-house promote/fence/reattach controller | fence/promote/reclone branches unit-tested with a fake `Promoter`; live promotion in integration |
| `timescale` | 79% | TimescaleDB enable + job listing | `±infinity` next-start handling + integration |
| `alerts` | 38% | alert-rule evaluation + persistence | rule eval unit-tested; delivery/store in integration |
| `metabackup` | 24% | meta-DB self-backup (pg_dump/restore) | key uniqueness + version parse unit-tested; round-trip in integration |
| `objectstore` | 18% | S3/MinIO object I/O | error-mapping unit-tested; real I/O in integration |
| `events` | 8% | durable control-plane event log | repo round-trips in integration |

### Regression tests added by the aggressive bug hunt (2026-06-03)

Every fix in [`bug-hunt-tracking.md`](bug-hunt-tracking.md) landed test-first. The
load-bearing regressions:

- **Failover split-brain** (`failover_test.go`): a failed fence must abort
  promotion (`TestFailoverAbortsIfFenceFails`); a zero-progress standby is not
  promotable (`TestFailoverWontPromoteZeroProgressStandby`); the most-caught-up
  replica is promoted, the old primary fenced (stopped **and** removed), and the
  other replicas re-cloned (`TestFailoverPromotesMostCaughtUpReplica`).
- **SQL console OOM** (`sql_test.go`): rows are bounded by a byte budget, not just
  a count, so one giant row can't exhaust memory (`TestCollectRowsByteBudget`).
- **Exec OOM/hang** (`exec_test.go`): output is capped and the call is timed
  (`TestExecBoundsOutput`, `TestExecAppliesTimeout`).
- **Dump password leak** (`dump_test.go`): the password goes via `PGPASSWORD`,
  never argv (`TestBuildDumpCmdHidesPassword`), and a mid-stream pg_dump failure is
  logged, not swallowed (`TestDumpLogsStderrOnFailure`).
- **Data-plane audit** (`audit_dataplane_test.go`): sql/exec/dump each write an
  audit record once their authz guard passes.
- **Same-second meta-backup** (`metabackup_test.go`): keys carry a crypto-random
  suffix so two backups in one second don't overwrite
  (`TestStampKeyUniquePerCall`); missing-key reads map to `KindNotFound`
  (`TestGetObjectErrorMapping`).
- **Config validation** (`config_test.go`): instance bind address, container
  restart policy, and alert webhook URL are validated at load (bind defaults to
  `127.0.0.1`, secure-by-default).

## Load & consistency testing at scale (`cmd/loadgen`)

`cmd/loadgen` is a standalone harness that drives a managed instance to a
millions-of-rows scale and then proves it stayed transactionally consistent under
concurrency. It is a client tool — point it at any instance DSN.

**The consistency oracle.** It seeds a fixed pot of money across N accounts
(`SUM(balance) = accounts × start-balance`). Concurrent workers then move money
between random accounts, each transfer wrapped in a single transaction that locks
both rows (lowest id first, to avoid deadlocks) and refuses to drive a balance
negative. No matter how many transfers run — or are rolled back — the pot total
must be unchanged. `verify` re-reads it and exits non-zero on any drift, a
negative balance, or an orphaned event, so it doubles as a CI assertion.

**The volume workload.** Alongside the transfers, workers run a weighted mix of
event `INSERT` / `UPDATE` / `DELETE` / `SELECT` against an append-heavy ledger
that grows into the millions, stressing autovacuum, indexes, bloat, and read/write
contention. Seeding uses `COPY` for throughput.

```bash
go build -o bin/loadgen ./cmd/loadgen

# seed + churn + verify in one shot
bin/loadgen \
  -dsn "$DATABASE_URL" \
  -mode all -accounts 100000 -events 5000000 \
  -workers 32 -duration 2m -drop

# or run phases independently against an already-seeded DB
bin/loadgen -dsn "$DATABASE_URL" -mode churn -workers 64 -duration 10m
bin/loadgen -dsn "$DATABASE_URL" -mode verify   # exit 1 ⇒ inconsistency
```

The DSN also reads from `$LOADGEN_DSN` / `$DATABASE_URL`. `SIGINT`/`SIGTERM`
cancels in-flight work cleanly. Run `verify` after a crash, a failover, or a
restore drill to prove no committed transaction was lost or torn.

## What the integration tests prove

The suite is **71 integration test functions across 25 files** (446 test
functions total across unit + integration). The load-bearing ones:

- **Point-in-time recovery** (`provision`): insert batch 1 → full backup →
  capture a timestamp → insert batch 2 → restore to the timestamp → assert
  *only batch 1* survives. The acceptance gate for the backup milestone.
- **Safe restore** (`provision`): restores into a fresh staging volume and
  swaps; the live data is never mutated and rolls back on failure.
- **Streaming replication** (`provision`): provisions a primary + replica,
  writes on the primary, confirms it replicates, and that the replica is a
  read-only standby — with a space/quote-containing password (the `.pgpass`
  path).
- **Query router** (`provision`): fronts a primary + replica with a real PgCat
  container and round-trips data through it.
- **Full cluster lifecycle** (`clusterctl`): create → provision primary +
  replica + router → query through the router → membership.
- **Restore drills & health** (`health`): restores the latest backup into a
  throwaway container and validates it with `pg_controldata`.
- **Migrations** (`store`): every migration applies, rolls back, and is
  idempotent on re-up against a real Postgres.
- Repository round-trips, conflict/not-found mapping, and cascade behaviour for
  every repo (`user`, `instance`, `cluster`, `backup`, `metrics`, `health`,
  `audit`).

## CI

`.github/workflows/ci.yml` runs three jobs on Go 1.25.7: **unit** (`go mod
tidy` check + `go vet` + `go test -race`), **lint** (golangci-lint v2), and
**integration** (`go test -race -tags=integration`). The frontend builds and
tests separately. The codebase is golangci-lint v2 clean (0 issues).
