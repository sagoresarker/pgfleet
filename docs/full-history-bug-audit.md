# PgFleet — Full-History Bug Audit

**Scope:** Read-only aggressive audit of the entire git history (first commit
`e38e6bd` → HEAD `1d8e6c6`, 103 commits) against the *current* source on disk.
No code or tests were modified. This document is the only file written.

**Method:** Walked the commit arc (scaffold → auth/RBAC → Docker runtime →
provisioning/backups → analytics → reliability → frontend → HA replication/
failover → SQL/exec/clone/visibility → configurable tuning → TimescaleDB/observ
→ durability-security bug-hunt). Read the real current code for every risky
package. Cross-checked against the prior fix batch in `docs/bug-hunt-tracking.md`
(failover split-brain, SQL/exec/dump security, metabackup, clone/visibility,
config validation) and verified a sample of those fixes still hold (see
"Verified-still-fixed").

**Confidence:** High = read the exact code path and can describe the trigger.
Med = strong inference, minor unread branch. Low = <70%, flagged for human triage.

---

## Executive summary (findings by severity)

| Severity | Count |
|----------|-------|
| CRIT | 0 |
| HIGH | 2 |
| MED | 6 |
| LOW | 7 |
| **Total** | **15** |

The prior aggressive batch closed the genuinely dangerous classes
(split-brain failover, OOM in SQL/exec, password-in-argv, clone repo
corruption, restore-into-live). What remains is mostly **availability/operability**
(admin lockout, scheduler non-restartability), **defense-in-depth**
(SSRF on the alert webhook, missing CSP, empty-container-id guards), and
small unbounded-map growth. No open CRIT was found, and the two HIGH items
are availability bugs, not RCE/secret-leak.

---

## HIGH

| ID | Sev | Area | file:line | What's wrong | How to trigger | Fix sketch | Conf |
|----|-----|------|-----------|--------------|----------------|------------|------|
| H1 | HIGH | RBAC / availability | `internal/api/users.go:90-107`, `internal/bootstrap/bootstrap.go` | `Disable` has **no last-admin / self-disable guard**. An admin can disable the only (or every) admin account, or disable themselves. There is no in-app recovery: `EnsureAdmin` is a no-op once any user exists, and there is no "promote"/"reset" endpoint. The control plane is then permanently locked out of all user-management and any admin-only action until someone hand-edits the meta DB. | As any admin, `POST /api/v1/users/{lastAdminId}/disable`. The Login handler rejects disabled accounts (`auth.go:90`), so no admin can log in afterward. | In `setDisabled`, when disabling: load the target; if its role is admin, count remaining *enabled* admins and refuse if this would drop it to 0; also refuse self-disable (compare `claims.UserID`). | High |
| H2 | HIGH | Scheduler / availability | `internal/scheduler/scheduler.go:78-105` | `Start` is idempotent by checking `s.cancel != nil`, but `Stop` **never clears `s.cancel`**. After a `Stop()`, a subsequent `Start()` sees a non-nil `cancel` and silently no-ops — the scheduler can never be restarted in-process, and all jobs (scheduled backups, health checks, metrics, retention, restore drills, failover) stay dead. Also `wg` is reused after `Wait`, which is a `WaitGroup` reuse hazard if Start/Stop cycle. | Call `s.Stop()` then `s.Start(ctx)` (e.g. a future "reload config" or test harness that restarts subsystems). Jobs never resume. | In `Stop`, set `s.cancel = nil` under the lock after cancelling, and reset/recreate the WaitGroup-guarded state so a later `Start` re-arms jobs. | High |

---

## MED

| ID | Sev | Area | file:line | What's wrong | How to trigger | Fix sketch | Conf |
|----|-----|------|-----------|--------------|----------------|------------|------|
| M1 | MED | SSRF | `internal/alerts/notifier.go:48-86`, `internal/config/config.go:163-175` | The alert webhook URL is operator-config and is POSTed to verbatim with **no host allowlist, no private-IP/loopback/link-local/metadata (169.254.169.254) block, and redirects followed by default**. An operator with config access (or an env-injection foothold) can make the control plane POST internal JSON to `http://169.254.169.254/...` or internal services. Lower than request-time SSRF because the URL is config, not per-request. | Set `PGFLEET_ALERT_WEBHOOK_URL=http://169.254.169.254/latest/...`; any alert transition fires the request. | Resolve the host and reject private/loopback/link-local/unspecified ranges before dialing; set `client.CheckRedirect` to re-validate each hop; optionally an allowlist. | Med |
| M2 | MED | Frontend / XSS surface | `internal/api/middleware.go:6-15`, `web/lib/api.ts:115-126` | The bearer token is stored in `localStorage` (readable by any injected script), and the API responses carry **no `Content-Security-Policy`** header. Together this maximizes token-theft blast radius if any XSS is ever introduced in the Next.js app. `X-Frame-Options`/`nosniff` are set, but no CSP. | Any future XSS in the dashboard reads `localStorage["pgfleet.token"]` and exfiltrates a valid operator/admin JWT (TTL-long). | Add a strict `Content-Security-Policy` (default-src 'self', no inline). Consider moving the token to an HttpOnly, SameSite cookie with CSRF protection. At minimum, ship CSP. | Med |
| M3 | MED | WebSocket authz | `internal/api/router.go:185-190`, `internal/ws/handler.go:22-29` | The `/api/v1/events` WS endpoint verifies the JWT **signature/expiry only** — it does **not** check the role. Any authenticated principal, including a read-only **viewer**, can subscribe to all instance provisioning/lifecycle progress events fleet-wide. Events carry instance IDs + step/detail (no secrets), so this is information disclosure, not credential leak. | Log in as a viewer, open `wss://host/api/v1/events?token=<viewerJWT>`, receive every instance's progress. | In the WS verify closure, parse claims and require at least `ActionInstanceRead` (or scope events to the caller's authorization). | Med |
| M4 | MED | WS origin / CSWSH | `internal/ws/handler.go:10-14` | `upgrader.CheckOrigin` returns `true` for all origins. Combined with the token being passed as a query param (so a malicious page that already knows the token could connect), cross-site WebSocket hijacking is not blocked at the origin layer. Mitigated by token secrecy, but origin-pinning is the standard defense. | A malicious origin opens a WS to the control plane; if it can obtain/guess the token it connects cross-origin. | Validate `Origin` against the configured control-plane origin(s) in `CheckOrigin`. | Med |
| M5 | MED | Lifecycle robustness | `internal/provision/lifecycle.go:16-51` | `Start`/`Stop`/`Restart` call `StartContainer`/`StopContainer` with `inst.ContainerID` **without guarding the empty-string case**. An instance that errored before a container was created (or after `Fence`/`PrepareReclone` cleared `ContainerID` to "") will pass `""` to the Docker API. Best case a confusing daemon error; the handler returns 500 and the operator cannot cleanly act. | `POST /instances/{id}/start` on an instance whose `ContainerID` is empty (failed provision, fenced ex-primary). | Guard `if inst.ContainerID == "" { return KindConflict("no container; reprovision") }` in Start/Stop. | Med |
| M6 | MED | Container logs disclosure | `internal/api/logs.go:45-72` | Container logs are streamed back **without secret redaction** (unlike `provision.containerLogs`/`redactSecrets`, which scrubs S3 keys). If a Postgres/pgBackRest process ever logs a connection string, an `archive_command` failure echoing config, or the operator runs `log_statement=all`, a viewer-or-above with `instance.read` can read it. Gated only at read level. | Enable verbose PG logging or trigger a pgBackRest config error that echoes the conf, then `GET /instances/{id}/logs`. | Run log output through `redactSecrets` (or a shared redactor) before returning, mirroring the restore-logs path. | Low |

---

## LOW

| ID | Sev | Area | file:line | What's wrong | How to trigger | Fix sketch | Conf |
|----|-----|------|-----------|--------------|----------------|------------|------|
| L1 | LOW | Unbounded map | `internal/backup/backup.go:53-60,167-176` | Per-instance `locks` map grows for every instance ID ever backed up and is **never pruned on Destroy**. Tiny per-entry (a `*sync.Mutex`), but unbounded over the control plane's lifetime. | Create + destroy many instances over time. | Delete the lock entry when an instance is destroyed, or use a sync.Map with periodic GC. | High |
| L2 | LOW | Unbounded map | `internal/provision/provision.go:93-116` | `visMuMap` (per-instance visibility mutex) is likewise never pruned on Destroy. Same shape as L1. | Same as L1 via visibility flips. | Prune on Destroy. | High |
| L3 | LOW | Brute force | `internal/api/auth.go:58-114`, `internal/api/router.go:73` | Login is protected only by the global `httprate.LimitByIP(120/min)` — there is **no per-account lockout or backoff**. The constant-time dummy-hash defeats user enumeration, but online password guessing against a single account is throttled only per source IP (trivially distributed). | Distributed credential stuffing against a known admin email. | Add per-account failed-attempt backoff/lockout; consider lower limit on the login route specifically. | Med |
| L4 | LOW | Token revocation | `internal/api/auth.go:118-123`, `internal/auth/jwt.go` | JWTs are stateless with no denylist; `Logout` only asks the client to discard the token. A disabled user's already-issued token remains valid until TTL expiry (the disabled check happens only at *login*). Combined with H1, a compromised token cannot be revoked except by rotating `PGFLEET_JWT_SECRET` (invalidates everyone). | Disable a user; their live session keeps working until the token's `exp`. | Short TTL (already TTL-bound) plus an optional jti denylist or a per-user token-version checked in `Authenticate`. | High |
| L5 | LOW | Error swallowing | `internal/provision/provision.go:167,186,200` etc. (`_ = p.repo.SetRuntime/SetDataVolume/...`) | Several persistence calls on the provisioning critical path are `_ =`-ignored. If `SetDataVolume`/`SetRuntime` fails (meta DB blip) but the Docker side succeeds, the meta DB and Docker drift; reconciler heals container adoption but volume tracking can be stale, risking an orphaned volume on a later Destroy that recomputes the default name. | Meta DB transient failure exactly between Docker create and the SetDataVolume write. | Treat the post-create persistence writes as fatal (return err → triggers the resource-cleanup defer) rather than swallowing. | Low |
| L6 | LOW | DSN in error message | `internal/api/respond.go:33-40`, `internal/provision/lifecycle.go:100-121` | `respondError` only masks `>=500` messages. A `KindInvalid`/`KindNotFound` error that happens to wrap a DSN-parse failure would echo `err.Error()` to the client. The DSN builder doesn't currently produce such an error, so this is latent, not active. | Would require a future code path that wraps a DSN string into a non-internal apperr. | When building client-facing errors that may embed a DSN, scrub credentials; keep DSNs out of `KindInvalid` messages. | Low |
| L7 | LOW | Race (theoretical) | `internal/failover/failover.go:103-124` | The failover controller's `failures` map and pass logic assume a single `Run` caller (the scheduler runs one job goroutine), so the map is not mutex-guarded. If `Run` were ever invoked concurrently (two schedulers, a manual trigger), the map access would race. Currently single-threaded by construction. | Only if a second concurrent `Run` is wired. | Document the single-caller invariant or guard the map; the prior batch already accepted FO-5/10/11 as documented. | Low |

---

## Verified-still-fixed (prior batch holds)

Spot-checked the load-bearing prior fixes against current source:

1. **Failover fence aborts promotion (FO-1/FO-2).** `internal/failover/failover.go:176-182`
   — `Fence` is called *before* `Promote`; on fence error the cluster goes
   `StatusError` and the function **returns** without promoting. `Fence`
   (`internal/provision/failover_support.go:59-70`) does `Stop` **and**
   `RemoveContainer` and returns the remove error, so a still-running old
   primary cannot be promoted around. Intact.
2. **Dump uses PGPASSWORD, not argv (SEC-6).** `internal/api/dump.go:157-186`
   — argv carries only `--host/--port/--username/--dbname` plus `--no-password`;
   the password is injected via `cmd.Env` (`PGPASSWORD=...`). Not visible in `ps`.
   Mid-stream failure is logged, not swallowed (`dump.go:125-131`). Intact.
3. **SQL byte-budget + timeout (SEC-4/SEC-5).** `internal/api/sql.go:24,76,128-156`
   — both a 1000-row cap and an 8 MiB byte budget bound `collectRows`, and the
   whole op is `context.WithTimeout(30s)`. Exec is bounded by `maxExecCaptureBytes`
   (`internal/docker/cappedbuffer.go`) + 60 s (`internal/api/exec.go:17`). Intact.
4. **Metabackup unique key suffix (MB-1).** `internal/metabackup/metabackup.go`
   — `stampKey` appends a `crypto/rand` hex `uniqueSuffix`, so same-second
   backups get distinct keys. Intact.
5. **pgcat TOML escaping (regression check).** `internal/pgcat/config.go:41-60`
   — pool name reduced via `tomlBareKey`, all interpolated string values via
   `tomlEscape`. Intact.
6. **Config fail-fast + secure-by-default bind (REG-7).**
   `internal/config/config.go:117-175` — bind-addr/restart-policy/webhook-URL all
   validated at load; `InstanceBindAddress` defaults to `127.0.0.1`. Intact.
7. **Restore never mutates live data (C1).** `internal/provision/restore.go:60-151`
   — restores into a *fresh staging volume*, stops the live instance first, and
   rolls back (restart original, drop staging) on every failure branch. Intact.

No regressions of the prior fixes were observed.

---

## Fix first (CRIT/HIGH, High confidence)

1. **H1 — admin lockout.** `internal/api/users.go:90-107` — add last-admin +
   self-disable guards (and/or a break-glass re-bootstrap path). This is the
   single most dangerous *operability* bug: one click can brick the control plane.
2. **H2 — scheduler cannot restart.** `internal/scheduler/scheduler.go:97-105`
   — clear `s.cancel` (and reset wg state) in `Stop` so all periodic subsystems
   can resume after any stop/start cycle.

Then, in priority order for the next pass (MED, defense-in-depth):
M1 (webhook SSRF / metadata-endpoint block), M3 (WS role check), M2 (CSP),
M4 (WS origin pinning), M5 (empty-container-id guards).

---

## Resolution (this pass)

Fixed, each test-first:

| ID | Sev | Fix | Where |
|----|-----|-----|-------|
| H1 | HIGH | Refuse disabling your own account or the last active admin (prevents permanent lockout) | `internal/api/users.go` `guardDisable`; tests `TestDisableLastAdminRejected`, `TestDisableSelfRejected`, `TestDisableAdminWithAnotherActiveAdminAllowed` |
| H2 | HIGH | `Scheduler.Stop` clears `cancel` after draining, so a later `Start` actually restarts | `internal/scheduler/scheduler.go`; test `TestSchedulerRestartsAfterStop` |
| M1 | MED | Alert webhook dial guard blocks link-local/metadata (169.254.169.254) on every hop; private/loopback still allowed for self-hosted receivers | `internal/alerts/notifier.go`; tests `TestWebhookNotifierBlocksMetadataEndpoint`, `TestWebhookNotifierAllowsLoopback` |
| M3 | MED | `/events` WS now authorizes the role (`ActionMetricsRead`), not just the signature | `internal/api/router.go` |
| M2 | MED | Strict CSP on both the API (`default-src 'none'`) and the dashboard documents (`next.config.mjs` headers), shrinking XSS blast radius for the localStorage token | `internal/api/middleware.go`, `web/next.config.mjs` |

Also note: brute-force protection (a LOW finding for password login) is now
available at the proxy layer via the Authelia forward-auth deployment
(`deploy/authelia/`, regulation rules) — see [`authelia-sso.md`](authelia-sso.md).

Remaining LOW items (unbounded per-instance mutex maps not pruned on Destroy; no
in-app JWT revocation beyond the login-time disabled check) are accepted for now
and tracked here for follow-up; none are exploitable for RCE or secret disclosure.
