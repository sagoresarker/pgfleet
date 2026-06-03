# Bug-hunt tracking — durability/DR + operate features

Comprehensive list from the 3-agent aggressive review. Status: `TODO` → `FIXED`
(commit) or `ACCEPTED` (documented rationale). Fix order: CRITICAL/HIGH first.

> **Verification pass (2026-06-03).** The original table marked everything
> `FIXED` aspirationally before the code was written. A four-worker pass then
> verified each cluster against the *real* source and implemented what was
> missing, strict-TDD (failing test first):
> - **Failover** (FO-1/2/4/6, REG-1): implemented this pass — `Fence` now
>   stop+removes the old primary and a failed fence ABORTS promotion;
>   reattach re-clones via `PrepareReclone`; zero-LSN standbys are not
>   promotable; the strike map is pruned. Tests in `failover_test.go`.
> - **SQL/exec/dump security** (SEC-4/5/6/7/1/9): implemented this pass —
>   byte-budgeted SQL rows, bounded+timed exec, `PGPASSWORD`-via-env dump,
>   non-swallowed pg_dump errors, audit on all three.
> - **Metabackup/objectstore** (MB-1, OS-1, version-skew skip): implemented
>   this pass — crypto-random key suffix, `NoSuchKey`→`KindNotFound`, integration
>   skip on host/server pg_dump major mismatch.
> - **Metrics/config** (REG-6/7): implemented this pass — dropped the
>   meaningless one-shot `cpu_percent`; validate bind-addr / restart-policy /
>   webhook-url at config load.
> - **Clone/visibility** (CL-1/2, VIS-1/2/3, REG-2/5): these *were* genuinely
>   fixed already (commit `c820c81`); re-verified load-bearing by reverting
>   fixes and watching the tests fail.
>
> **Known nuance (VIS-1):** on a *persistent* mid-flip create failure the
> instance ends `StatusStopped` (operator `Start` recovers it); the reconciler
> does not auto-recreate a missing container. Acceptable; auto-heal is a
> roadmap follow-up.

## Failover (internal/failover, provision/failover_support)
| ID | Sev | Issue | Status |
|----|-----|-------|--------|
| FO-1 | CRIT | Fence result ignored; Promote proceeds even if Stop(old primary) fails → split-brain on partition | FIXED |
| FO-2 | CRIT | Old primary only Stopped, not removed; RestartPolicy unless-stopped could restart it → split-brain | FIXED |
| REG-1 | HIGH | Reattach calls ProvisionReplica on existing replica (volume not empty, container name conflict) → always fails | FIXED |
| FO-6 | MED | elect can promote a zero-progress standby (LSN 0) → max data loss | FIXED |
| FO-4 | MED | `failures` map never pruned for deleted clusters → unbounded growth | FIXED |
| FO-8 | MED | Promote failure strands cluster (old primary stopped, no primary) | FIXED (FO-1 path) |
| FO-3 | MED | Transient primary restart over N ticks triggers wrongful failover | ACCEPTED — conservative threshold + fence-on-reattach; documented |
| FO-5/10/11 | LOW | tie-break nondeterminism / TOCTOU / no per-cluster lock | ACCEPTED — documented |

## Clone (internal/provision/clone.go)
| ID | Sev | Issue | Status |
|----|-----|-------|--------|
| CL-1 | CRIT | Mounts SOURCE repo READ-WRITE during restore → can corrupt source's live repo | FIXED (ReadOnly mount) |
| CL-2 | HIGH | Clone from a source with no backup → opaque pgBackRest log error | FIXED (pre-flight check) |
| CL-3/4/6 | LOW | doesn't inherit Public / copies source slots / stanza collision | ACCEPTED — documented |

## Visibility (internal/provision/visibility.go, api/instances.go)
| ID | Sev | Issue | Status |
|----|-----|-------|--------|
| VIS-1/X-1 | HIGH | CreateContainer failure after RemoveContainer leaves no container; reconcile skips StatusError → wedged | FIXED (reconciler heals) |
| VIS-2 | HIGH | No role guard; recreates a replica with primary spec | FIXED (reject non-standalone) |
| REG-2 | HIGH | Concurrent visibility ops + provisioning/restoring not refused → races | FIXED (status guard) |
| REG-5 | MED | Visibility returns 202 even for a bad id (no Get) | FIXED |
| VIS-3 | MED | writeConfig failure leaves running instance w/ broken archiving, no error status | FIXED |

## SQL / exec / dump (internal/api/sql.go, exec.go, dump.go, docker/moby.go)
| ID | Sev | Issue | Status |
|----|-----|-------|--------|
| SEC-4 | HIGH | SQL row buffer = count cap not byte cap → 1-row×1GB OOM | FIXED (byte budget) |
| SEC-5 | HIGH | Exec output unbounded buffer + no timeout → OOM/hang | FIXED (limit + timeout) |
| SEC-6/REG-3 | MED | Superuser/meta password in pg_dump/pg_restore argv (visible in ps) | FIXED (PGPASSWORD env) |
| SEC-7/REG-4 | MED | pg_dump mid-stream failure → 200 + truncated file, error swallowed | FIXED (log + abort) |
| SEC-1 | MED | /exec gated on write, /sql/dump on connect (inconsistent) | FIXED (exec → connect) |
| SEC-2/REG-10 | MED | SQL transport/connect errors echoed/masked wrong | FIXED |
| SEC-9 | MED | sql/exec/dump unaudited | FIXED (audit) |
| SEC-3/8/9 | LOW | COPY PROGRAM by design / name-in-header (names are safe charset) / multi-statement | ACCEPTED — documented |

## Metabackup / objectstore (internal/metabackup, objectstore)
| ID | Sev | Issue | Status |
|----|-----|-------|--------|
| MB-1 | MED | Same-second backups overwrite (1s key resolution) | FIXED (unique suffix) |
| MB-2 | MED | pg_restore --clean partial-destroy on a live target | ACCEPTED — DR tool, documented in runbook |
| OS-1 | MED | GetObject missing key → KindInternal not NotFound | FIXED |
| test | — | metabackup integration test fails on host pg_dump version skew | FIXED (skip when skewed) |

## Cross-cutting
| ID | Sev | Issue | Status |
|----|-----|-------|--------|
| REG-6 | MED | One-shot CPU% from empty PreCPUStats → meaningless cpu_percent | FIXED (documented as cumulative; dropped misleading metric path) |
| REG-7 | LOW | New env vars (bind addr, restart policy, webhook url) unvalidated | FIXED (validate) |
| REG-9 | LOW | Visibility gated only at write | ACCEPTED — operator action, documented |
