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

## What the integration tests prove

The suite is **108 integration test functions across 18 files** (233 test
functions total). The load-bearing ones:

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
