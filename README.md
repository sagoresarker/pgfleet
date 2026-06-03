# PgFleet

A self-hosted **managed-Postgres control plane** — a focused, open-source alternative to RDS / Crunchy Bridge that you run yourself with Docker.

From one professional web UI an operator can:

- **Provision** Postgres instances, each in its own Docker container.
- Get automatic **WAL archiving + scheduled backups** to object storage (MinIO/S3) or a local volume, powered by [pgBackRest](https://pgbackrest.org/).
- **Restore & PITR** (point-in-time recovery) through a guided timeline wizard — restores into a fresh volume and swaps, so live data is never at risk.
- Run **high-availability clusters**: a primary with streaming read replicas behind a [PgCat](https://github.com/postgresml/pgcat) query router that splits reads and writes.
- Watch **live + historical analytics** (connections, throughput, query insights from `pg_stat_statements`).
- Trust the reliability story: verified backups (automated restore drills), archiving-health + `pg_wal` alerts, and a crash-safe control plane that reconciles on restart.

**[→ Quickstart](docs/QUICKSTART.md)** · **[Testing & coverage](docs/TESTING.md)**

## Architecture

| Layer | Tech |
|-------|------|
| Backend (control plane) | Go — chi, pgx, Docker Engine API, JWT + RBAC |
| Managed instances | `postgres:16` + pgBackRest, one stanza per instance |
| Replication / routing | streaming replicas + PgCat (read/write split) |
| Backup repo | MinIO (S3-compatible) by default; local volume supported |
| Meta DB | A dedicated Postgres storing users, instances, clusters, backup catalog, metrics, health, audit |
| Frontend | Next.js (App Router) + Tailwind v4 + Radix + React Query |

## Prerequisites

- **Docker** (Desktop or Engine) running — PgFleet provisions each Postgres instance as a sibling container on this host.
- **Go 1.25+ and Node 20+** — only if you build/run from source.

**Build the managed Postgres image once — nothing is pushed to a registry.** Every instance runs from a custom `postgres + pgBackRest` image you build **locally**:

```bash
make image     # builds pgfleet/postgres-pgbackrest:16
make images    # or build every supported version (PG 13–17)
```

The control plane finds this image on the **local** Docker daemon and uses it directly — you never push it anywhere. PgCat (`ghcr.io/postgresml/pgcat`) and the base `postgres` / `minio` images are pulled automatically from public registries. *(A registry is only needed for a multi-host setup, where the control plane provisions onto **remote** Docker daemons — then push the image and point instances at that ref.)*

## Quickstart

```bash
make dev-up                       # meta-DB + MinIO
make image                        # build the managed postgres+pgBackRest image (local only)
cp .env.example .env && make run  # run the control plane on :8080
cd web && npm install && npm run dev   # console on :3000
```

Full walkthrough in [docs/QUICKSTART.md](docs/QUICKSTART.md).

## Development

```bash
make test             # fast unit tests (no Docker)
make test-integration # integration tests (real Postgres + MinIO + pgBackRest)
make build            # build the API server
make image            # build the managed-instance image
make lint             # golangci-lint v2
```

Built strictly test-first (TDD), commit by commit. Combined unit + integration
coverage is **78.3%**; see [docs/TESTING.md](docs/TESTING.md).
