# Bug Hunt 4 — exhaustive line-by-line pass (4 agents, frontend + backend)

"Do not tolerate any small/tiny bug." Four agents read every file line-by-line
(frontend, API handlers, core/provision, store/metrics/auth). The codebase was
again confirmed solid; the real issues found are below — **all fixed**, verified
with build + lint(0) + frontend tsc/build/vitest + cluster-lifecycle / PITR /
drill integration tests green.

## Frontend

| Sev | Issue | Fix |
|-----|-------|-----|
| HIGH | `users/page.tsx` called `useState`/`useMemo` AFTER an early `return` (Rules of Hooks) → crash for non-admins / on role change. | Moved all hooks above the early return. |
| HIGH | `status.tsx` `statusMap[status]` undefined for any status outside the union → crashed every list row. | Fallback to a neutral/idle badge. |
| MED | `metrics-viz` `sumRates` zipped commit/rollback rate series by index → silently wrong TPS on a counter reset. | Merge by timestamp. |
| MED | `restore-dialog` `set` captured `backups[0]` once at mount; async-loaded backups left it `""` → "set" restore posted an empty label. | `useEffect` syncs the selection. |
| MED | `api.ts` 401 handler hard-redirected for `/auth/sso` too → broke the SSO-unavailable inline message. | Skip redirect for both auth probes. |
| MED | `formatBytes` produced "NaN undefined" for negative / ≥PB values. | Clamp index, guard non-finite; +PB/EB units; test added. |
| LOW | connections gauge max could be 0; routing effect listed unstable array deps; `#timescaledb` deep-link → blank panel; active-alert row showed raw seconds; compose-preview clipboard threw on insecure origins; dashboard alert used index keys. | All fixed. |

## Backend

| Sev | Issue | Fix |
|-----|-------|-----|
| HIGH | `reconcile` indexed containers by `LabelInstance` only, so a transient helper (base-backup/restore/drill/router) was adopted as the instance's MAIN container and, on exit, flipped it to `error`. | Skip transient-role containers. |
| HIGH (op) | Global rate limiter throttled `/healthz`, `/readyz`, `/metrics` → probe flaps & dropped scrapes behind a few source IPs. | Apply the limiter to the `/api/v1` group only. |
| MED | `redactSecrets` didn't scrub `repo2-cipher-pass` (now reachable) → repo2 passphrase could leak into `last_error`/logs. | Added to the redact list. |
| MED | `moby.Inspect` dereferenced `NetworkSettings` without a nil check → panic on an odd-state container. | Nil-guard. |
| MED | Replica containers ignored `BindAddress` (published on 0.0.0.0), had no `RestartPolicy`, and lacked recovery labels. | Mirror the primary spec. |
| MED | Restore drill mounted the live repo READ-WRITE and recursively chowned it, racing live backups. | Read-only mount; chown only the drill data dir. |
| MED | Catalog `sync` pruned to empty when a present-but-UNHEALTHY stanza reported zero backups → hid restorable backups. | Skip prune when unhealthy + zero. |
| MED | Failover swallowed `SetPrimary`/`SetRole` errors after a successful promotion → meta/physical primary mismatch. | Log + mark cluster degraded. |
| MED | `Instances.Create` / `Clusters.Create` spawned `Provision` with no timeout on a `context.Background()` async base → goroutine could outlive shutdown drain and write to a closed pool. | Bounded with `context.WithTimeout`. |
| MED | Alert-rule precedence: a global rule could override an instance-specific rule depending on creation time. | `ORDER BY (instance_id IS NULL) DESC, created_at ASC`. |
| MED (sec) | `metabackup` passed the meta-DB DSN (with password) in `pg_dump`/`pg_restore` argv (visible via `ps`). | Password via `PGPASSWORD` env; stderr redacted. |
| LOW | Nil slices serialized as `null` (`pool.routing/*`, `metrics.samples`, `health.reports`); `metrics` `?limit` unbounded; bearer scheme case-sensitive; `DefaultRepoType`/`BackupType` unvalidated at boot. | All fixed. |

## Verified correct (not bugs)
- pgcat `SHOW SERVERS` uses `database_name`/`address_id` while `SHOW CLIENTS` uses `database` — this asymmetry matches PgCat's actual admin schema (confirmed against PgCat source), so the parsers are right.
- Auth/crypto, RBAC grants, SQL parameterization & scan order, migrations, scheduler, SSRF guards, secrets envelope — all re-confirmed safe.

## Deferred (low, documented)
- Backups `Create`/`Restore` don't pre-check instance existence (returns 202 then no-ops) — IDOR-lite in a single-org model.
- `PostgresConf` is dead, divergent code (remove or reconcile).
- Annotation byte-truncation rune-safety; unbounded per-instance mutex maps; optional SSO fail-closed-without-secret.
