# PgFleet

A self-hosted **managed-Postgres control plane** — a focused, open-source alternative to RDS / Crunchy Bridge that you run yourself with Docker.

From one professional web UI an operator can:

- **Provision** single-node Postgres instances, each in its own Docker container.
- Get automatic **WAL archiving + scheduled backups** to object storage (MinIO/S3) or a local volume, powered by [pgBackRest](https://pgbackrest.org/).
- **Restore & PITR** (point-in-time recovery) through a guided wizard.
- Watch **live + historical analytics** (connections, throughput, query insights from `pg_stat_*`).
- Trust the reliability story: verified backups (automated restore drills), archiving-health alerts, and a crash-safe control plane.

## Architecture

| Layer | Tech |
|-------|------|
| Backend (control plane) | Go — chi, pgx, Docker Engine API |
| Managed instances | `postgres:16` + pgBackRest, one stanza per instance |
| Backup repo | MinIO (S3-compatible) by default; local volume supported |
| Meta DB | A dedicated Postgres storing users, instances, backup catalog, metrics, audit |
| Frontend | Next.js (App Router) + React + Tailwind + shadcn/ui + React Query |

See [`docs`](docs/) and the implementation plan for details.

## Development

```bash
make dev-up           # start meta-DB + MinIO
make test             # fast unit tests (no Docker)
make test-integration # integration tests (Docker required)
make build            # build the API server
make lint             # golangci-lint
```

Built strictly test-first (TDD), commit by commit.
