# Bug Hunt 3 — pre-production audit (4 parallel deep-audit agents)

Scope: full codebase (security/auth, backup/restore data-safety, orchestration/
concurrency, storage/API/new-code). The codebase was found **fundamentally
sound** — JWT/argon2/injection-boundaries/RBAC-wiring/secret-scrubbing/IDOR, the
ws hub, scheduler, async tracker, failover quorum, DB/reader cleanup, and
migrations 00017–00020 were all verified safe. The real findings below.

## Fixed this pass (verified: unit + lint + PITR/clone integration green)

| ID | Sev | Issue | Fix |
|----|-----|-------|-----|
| C1 | CRIT | Restore not serialized — two concurrent restores (double-submit / restore racing a visibility flip) could delete each other's data volumes and leave the instance pointing at a removed volume (total live-data loss). | Per-instance op mutex (`instanceOpMutex`) now guards restore **and** visibility flips. `provision/restore.go`, `provision.go`, `visibility.go`. |
| C2 | CRIT | Reconciler races failover: during reclone the replica stayed `StatusRunning` with no container, so a 30s reconcile tick flipped it to `error` mid-failover. | Set `StatusProvisioning` before `PrepareReclone`; `ProvisionReplica` also stamps it at entry. Reconciler already skips provisioning. `failover.go`, `provision/replica.go`. |
| H1 | HIGH | Second repo (repo2) written **unencrypted** when at-rest encryption is on — silent confidentiality breach. | Emit `repo2-cipher-type`/`repo2-cipher-pass` when encrypted. `pgbackrest/config.go` + test. |
| H2 | HIGH | In-place restore mounted the live repo RW and archived a divergent post-promotion timeline into it **before** the swap committed → repo pollution on rollback. | Restore container starts recovery with `archive_mode=off`; the committed instance archives normally. `provision/restore.go`. |
| H3 | HIGH | `backup.Expire` (destructive retention sweep) took no lock and had no replica guard. | Added `lockFor` + `RoleReplica` guard, matching Delete/Verify. `backup/backup.go`. |
| H4 | HIGH | `Destroy` ran on the request context → a client disconnect mid-teardown orphaned data/repo volumes and stuck the row in `error`. | `context.WithoutCancel` detaches teardown. `api/instances.go`, `api/clusters.go`. |
| H5 | HIGH | SSO trusted-header identity accepted with zero proxy provenance — a direct off-proxy request could forge `Remote-Email`+`Remote-Groups` → instant admin. | Optional `PGFLEET_SSO_SHARED_SECRET`: proxy must present `X-Pgfleet-Sso-Secret` (constant-time), fail-closed; startup warns if unset. `api/sso.go`, `config.go`, `main.go` + test. |
| M1 | MED | Cloning an encrypted instance produced an **unencrypted** repo. | Clone inherits `source.Encrypted`. `provision/clone.go`. |
| M2 | MED | Cluster compose bound every member to host `:5432` → `docker compose up` fails ("port already allocated") with ≥1 replica. | Distinct host ports (primary 5432, replicas 5433…). `composegen/compose.go` + test. |
| M3 | MED | `--set` restore with no PITR type replayed WAL to the end of the archive, silently overshooting the chosen backup. | `--type=immediate --target-action=promote` stops at the backup's consistency point. `pgbackrest/cmd.go` + test. |
| M4 | MED | WebSocket upgrader accepted any Origin. | Same-origin check (absent Origin allowed for non-browser clients). `ws/handler.go`. |

## Deferred to the next exhaustive (line-by-line) pass

- **Alert-rule severity ignored**: `alerts.EffectiveThresholds` applies the rule's threshold but not its `severity`; firing severity comes only from hardcoded escalation cutoffs. Honor per-kind severity or drop it from the model.
- **`--repo=N` restore doesn't pin archive-get**: recovery's `restore_command` (archive-get) can still fall back to repo1 — exactly the repo you're recovering *around*. Pin the restore container's conf to repo N.
- **PITR `time` target timezone**: a bare local timestamp is interpreted in the restore container's TZ (UTC). The UI already emits `+00`; the API should reject/normalize bare timestamps.
- **Backup reference[] chain not persisted**: deleting a `full` cascade-expires dependents (handled by re-sync) but the operator gets no warning because the chain isn't surfaced.
- **Login rate-limit collapses behind the proxy**: the single global `LimitByIP` becomes one shared bucket; add a dedicated, tighter `/auth/login` throttle and/or per-account lockout.
- **Unbounded per-instance mutex maps** (`opMuMap`, backup `locks`) never prune on destroy — slow growth over a long-lived process.
- **Annotation truncation** slices on bytes (`s[:200]`) and can split a UTF-8 rune.
- **Stale runtime/volume refs** persisted before the provision cleanup defer removes the Docker objects.
