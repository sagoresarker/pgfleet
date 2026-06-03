# PgFleet — Bug Hunt 2 (advanced backups + PgCat + failover quorum + dashboard)

**Scope:** Read-only aggressive audit of the *current* source on disk, focused on
everything added in the last ~10 commits (HEAD `ecbf9a0` back through `656810d`):
advanced pgBackRest (block-incremental, delta, annotations, repo2, verify,
single-backup delete), encrypted backups + the derived cipher/router-admin
secrets, failover quorum guard, PgCat pool-mode/rw-split/mirroring/pool-stats,
clone auto-backup, cluster destroy guard, audit log, databases/roles tabs, disk
IO metrics, and the dashboard redesign. No code or tests modified; this file is
the only write.

**Method:** Walked the recent diffs (`git show` per commit), then read the real
current code for every risky package: `internal/provision/{cipher,router,
provision,clone,restore,lifecycle}.go`, `internal/pgbackrest/{config,cmd,info}.go`,
`internal/backup/{backup,catalog}.go`, `internal/failover/failover.go`,
`internal/clusterctl/service.go`, `internal/pgcat/{config,stats}.go`,
`internal/api/{router,backups,pool,audit,async,instances,respond}.go`,
`internal/ws/hub.go`, `internal/metrics/resource.go`, `internal/config/config.go`,
and the dashboard pages. Cross-checked `docs/full-history-bug-audit.md` and
`docs/bug-hunt-tracking.md` so nothing already-fixed is re-reported; verified a
sample of prior fixes still hold (see "Prior fixes re-verified").

**Confidence:** High = read the exact path and can state the trigger. Med =
strong inference, one unread branch. Low = <70%, flagged for human triage.

---

## Executive summary (findings by severity)

| Severity | Count |
|----------|-------|
| CRIT | 0 |
| HIGH | 2 |
| MED | 6 |
| LOW | 6 |
| **Total** | **14** |

The genuinely scary classes from the prior batches stay closed (split-brain
fence-before-promote, SQL/exec OOM, password-in-argv, clone repo corruption,
CSP, last-admin lockout — all re-verified). The new features add mostly
**durability/correctness gaps**: a global encryption flag that silently breaks
encrypted repos when toggled (H1), a single-backup delete that leaves orphan
catalog rows when pgBackRest cascade-expires dependents (H2), a replica
direct-destroy that leaks the replication slot (bounded by a WAL cap), and a
pair of **dead/half-wired features** (PgCat pool-mode + mirroring are plumbed
through `RouterSpec` but no caller ever sets them; backup annotations are
written but never persisted/read back). No new RCE or secret-leak: the derived
secrets are sound HMACs, the cipher pass is redacted in provision logs, and
internal errors are masked at the HTTP layer.

---

## HIGH

| ID | Sev | Area | file:line | What's wrong | How to trigger | Fix sketch | Conf |
|----|-----|------|-----------|--------------|----------------|------------|------|
| N1 | HIGH | Data durability / encrypted backups | `internal/provision/provision.go:336-338`, `internal/pgbackrest/config.go:94-97` | `BackupEncryption` is a **process-global env flag**, but the pgbackrest.conf is **regenerated from the live flag on every config write** — not just at provision. `writeConfig`/`backrestConf` runs on **restore** (`restore.go:136,181`), **visibility flips** (`visibility.go:114,151`), **clone** (`clone.go:172`), **replica/drill**. The cipher line is emitted only `if p.opts.BackupEncryption`. So if an operator provisions an instance with `PGFLEET_BACKUP_ENCRYPTION=true` (cipher fixed at stanza-create) and later restarts the control plane with it **unset/false**, the next restore/visibility/reclone writes a conf **without** `repo1-cipher-pass` → pgBackRest cannot read the encrypted repo (restore-into-staging fails, archive-get fails) → that instance is **unrecoverable from backup** until the flag is restored. The inverse (flag flipped on for an instance created without encryption) also breaks reads. The in-code comment claims "only affects instances first provisioned while on", but nothing persists per-instance encryption state. | Provision with encryption on; later restart the API without the env var (or vice versa); trigger any restore/visibility/clone → "unable to load info file … cipher" failure. | Persist per-instance encryption state (a bool/`cipher_enabled` column or infer from the stanza) and derive `CipherPass` from that, **not** from the live global flag. Refuse to start if the flag disagrees with existing stanzas. | High |
| N2 | HIGH | Data durability / backup delete | `internal/backup/backup.go:159-176`, `internal/backup/catalog.go:62-71` | `Delete` runs `pgbackrest expire --set=<label>` then `catalog.Delete(label)` for **only that one label** and **never re-syncs**. But `expire --set` of a **full** backup in pgBackRest **cascade-deletes its dependent diff/incr backups** from the repo. The catalog keeps **orphan rows** for those now-deleted dependents (and for any WAL-pruned sets), so `GET /backups` shows backups that no longer physically exist. An operator can then attempt a restore from a phantom set → opaque failure during an incident. The orphans only clear on the next scheduled `Sync`. | Take full → incr → incr; `DELETE /instances/{id}/backups/{fullLabel}`; the two incrementals vanish from the repo but remain in the catalog/UI as restorable. | After the `expire --set` exec succeeds, call `s.sync(ctx, inst)` instead of a single-label `catalog.Delete`, so the catalog is reconciled to exactly what pgBackRest still has (Upsert + Prune handles the cascade). | High |

---

## MED

| ID | Sev | Area | file:line | What's wrong | How to trigger | Fix sketch | Conf |
|----|-----|------|-----------|--------------|----------------|------------|------|
| N3 | MED | Dead feature / config drift | `internal/provision/router.go:36,41,122,126,128`, `internal/clusterctl/service.go:187-195`, `internal/failover/failover.go:296-304` | `RouterSpec.PoolMode` and `RouterSpec.Mirrors` are fully plumbed into `routerConfig`→`pgcat.Generate`, but **no caller ever sets them.** `clusterctl.provision` and `failover.repointRouter` build the `RouterSpec` without `PoolMode`/`Mirrors`, the cluster-create API/Input has no field for them, and the dashboard only *displays* `pool_mode` from stats. So "session vs transaction pool mode" and "query mirroring" are effectively **dead config** — the router always uses defaults. Operators believe the feature exists (commit message, UI label) but cannot configure it. (`grep` confirms the only setters are inside `router.go` itself.) | Create a cluster; inspect the generated pgcat.toml — `pool_mode` is always `transaction`, no `[mirrors]` ever. | Either wire `PoolMode`/`Mirrors` from `clusterctl.Input` (and persist them so failover re-applies) or remove the unused fields. Also failover **drops** any future pool config because it rebuilds the spec from scratch. | High |
| N4 | MED | Feature incomplete / annotation read-back | `internal/backup/catalog.go:23-44,74-94`, `internal/backup/backup.go:34-38`, `internal/api/backups.go:101` | Backup **annotations** are written to pgBackRest (`--annotation=name=…`, `cmd.go:59-61`) and parsed back by `ParseInfo` (`info.go:114`), but the catalog **`Upsert` never persists them** (no column in the INSERT) and `List` never selects them. So `Backup.Annotations` from the catalog is **always nil**, and the API `List` returns an empty `annotations` map. The user-supplied backup name never reaches the UI list. The code comment (`backup.go:36-37`) acknowledges the missing migration. | Create a backup with `{"annotation":"nightly"}`; `GET /instances/{id}/backups` → `annotations` is absent/empty. | Add an `annotations jsonb` column (migration), persist in `Upsert`, select in `List`, scan into `Backup.Annotations`. | High |
| N5 | MED | Durability / slot leak | `internal/api/instances.go:308-314`, `internal/provision/lifecycle.go:66-96` | The cluster-destroy guard refuses destroying a cluster **primary** directly, but **allows directly destroying a cluster REPLICA** via `DELETE /instances/{id}`. That path calls instance-level `Provisioner.Destroy`, which does **not** drop the replica's replication slot on the primary (only `clusterctl.Destroy` does, `service.go:296-298`). The orphaned slot pins WAL on the primary. Mitigated — not catastrophic — because the primary is started with `max_slot_wal_keep_size=10GB` (`provision.go:269`), so the slot is invalidated rather than filling the disk; but it still wastes up to 10 GB and leaves a dead slot. | In a cluster, `DELETE /instances/{replicaId}`; the slot `pgfleet_<replica>` lingers on the primary. | Mirror the primary guard: refuse direct destroy of any instance with a non-empty `ClusterID` (route operators through cluster member management), or drop the slot in instance-level Destroy when `ClusterID` is set. | High |
| N6 | MED | SSRF / migrate-in | `internal/api/remote.go:140`, `cmd/pgfleet-api/remote_target.go` | The remote (migrate-in) backup connects to an **operator-supplied host:port** as the control plane (a Postgres dial). No private-IP/loopback/link-local/metadata block. Same class as the prior alert-webhook SSRF (M1), but per-request rather than config. Lower because it speaks the PG wire protocol (not arbitrary HTTP) and is write-RBAC gated; still lets an operator-with-write probe internal services / `169.254.169.254:5432`. | `POST /remote/backups {host:"169.254.169.254", …}` to probe an internal port. | Reuse the alert-webhook dial guard to reject link-local/metadata (and optionally private) ranges before connecting, mirroring `notifier.go`. | Med |
| N7 | MED | Frontend / misleading invariant | `web/app/(app)/instances/[id]/page.tsx:714-715,771-775`, `internal/api/sql.go:86` | The Create-Database/Role modals build SQL by **client-side string concatenation** and the comment says the identifier is "quoted server-side via the SQL runner" — but `SQLHandler.Run` executes `conn.Query(ctx, req.Query)` **verbatim**; there is **no** server-side quoting/validation. Safety rests entirely on the client regex (`^[a-z_][a-z0-9_]{0,62}$`) and a disabled button. Because `/sql` runs as superuser and is operator/admin-gated, this is **not** a privilege boundary (the operator can already run any SQL), but the false invariant is a trap: a future caller that trusts "server-side quoting" would be wrong, and the regex gate is the *only* thing preventing a `"`-injected identifier. | Construct a request bypassing the disabled button (the regex only gates the button, not `mutate`) with a crafted `name` → raw SQL reaches the DB. Not an escalation, but breaks the stated contract. | Quote identifiers server-side with `pgx.Identifier{…}.Sanitize()` for the DB/role flows (add dedicated endpoints), or fix the comment and treat `/sql` as raw operator SQL only. | Med |
| N8 | MED | Failover correctness | `internal/failover/failover.go:288-312` | On failover, `repointRouter` rebuilds the router `RouterSpec` from scratch and **omits `PoolMode`/`Mirrors`** (and there is no way to set them anyway — see N3). It also re-derives the admin password correctly. The net effect today is benign (those fields are unused), but it bakes in the assumption that a router has no persisted pool config; the moment N3 is fixed, **every failover silently reverts pool mode and mirroring to defaults**. | Configure pool mode (once N3 is wired); trigger a failover → router comes back as transaction-mode, mirrors gone. | When repointing, load the cluster's persisted pool config and re-apply it to the new `RouterSpec`. | Med (latent) |

---

## LOW

| ID | Sev | Area | file:line | What's wrong | How to trigger | Fix sketch | Conf |
|----|-----|------|-----------|--------------|----------------|------------|------|
| N9 | LOW | Config / no fail-fast | `internal/config/config.go:102`, `internal/pgbackrest/config.go:108-112` | `PGFLEET_REPO2_PATH` is taken verbatim with **no validation** (not checked for being an absolute path, non-empty-after-trim, or distinct from repo1). A stray/relative value silently produces a misconfigured `repo2-path` and every backup quietly mis-targets the second repo (3-2-1 durability claim violated without an error). Other new flags are fine (`MasterKey` is length-validated; `BackupEncryption`/`BlockIncr` are booleans). | Set `PGFLEET_REPO2_PATH=relative/dir`; backups write repo2 to an unexpected location. | Validate at load: require an absolute path, trimmed, `!= repo1 path`; fail fast like the bind-addr/webhook checks. | High |
| N10 | LOW | Secret in server logs | `internal/backup/backup.go:316-318` | The backup package's `execOK` returns `res.Stderr+res.Stdout` **unredacted** on a non-zero exit. Unlike `provision.execOK` (which runs output through `redactSecrets`, `provision.go:399-400` scrubbing `repo1-cipher-pass`/`repo1-s3-key-secret`), a failing pgBackRest backup/expire/verify that echoes the conf could surface the cipher pass / S3 secret. Client impact is nil (it's `KindInternal` → masked to "internal error" by `respondError`), but it lands in server logs/events verbatim. | Induce a pgBackRest config error during backup that echoes the conf. | Run the backup `execOK` failure text through the shared `redactSecrets` before wrapping. | Med |
| N11 | LOW | Frontend / unvalidated deep-link | `web/app/(app)/instances/[id]/page.tsx:79-82` | The tab deep-link effect does `if (h) setTab(h)` for **any** hash value. A link to `#bogus` sets `tab` to an unknown id, so no tab panel matches and the operator sees an empty content area with no active tab. | Open `/instances/<id>#doesnotexist`. | Validate `h` against the known tab ids; fall back to `overview`. | High |
| N12 | LOW | SSO exposure if proxy bypassed | `internal/api/sso.go:77-91`, `internal/api/router.go:100-102` | `/api/v1/auth/sso` is mounted **unauthenticated** and trusts `Remote-Email`/`Remote-Groups` headers. This is the standard forward-auth design (the proxy must strip client copies and is the only ingress), already documented in `authelia-sso.md`. But if PgFleet is ever exposed without the proxy in front, anyone can POST `Remote-Email: admin@…` and mint an admin token. | Reach the API directly (no proxy) and send a spoofed identity header. | Bind the SSO listener to a separate internal interface, and/or require a shared secret header the proxy injects; keep the deployment caveat prominent. | Med |
| N13 | LOW | WS authz scope | `internal/api/router.go:223-233` | The `/events` WS now correctly requires `ActionMetricsRead` (prior M3 fix holds), but events are **fleet-wide**: any metrics-reader sees every cluster/instance's progress (no per-resource scoping). Information disclosure only (instance ids + step/detail, no secrets). | Viewer-with-metrics-read subscribes, sees all clusters' provisioning. | Scope the stream to resources the principal may read, if multi-tenant isolation is ever needed. | Med |
| N14 | LOW | Annotation value validation | `internal/pgbackrest/cmd.go:59-61`, `internal/api/backups.go:56,83` | The backup `Annotation` value flows unvalidated into argv as `--annotation=name=<value>`. It's argv (no shell), so no injection, but an empty/whitespace or very long value is accepted; a value containing `\n` would make pgBackRest's stored annotation odd. Cosmetic / robustness only. | `POST …/backups {"annotation":"a\nb"}`. | Trim and length-bound the annotation; reject control chars. | High |

---

## Prior fixes re-verified (still hold)

1. **Fence-before-promote (FO-1/2).** `internal/failover/failover.go:224-230` — `Fence` runs before `Promote`; on fence error the cluster goes `StatusError` and the function returns without promoting. Intact.
2. **CSP both layers (M2).** `internal/api/middleware.go:17` (`default-src 'none'; frame-ancestors 'none'; base-uri 'none'`) and `web/next.config.mjs:5-17`. Intact.
3. **Last-admin / self-disable guard (H1).** `internal/api/users.go:102,120,143` (`guardDisable` → "cannot disable the last active admin"). Intact.
4. **Cipher/S3-secret redaction.** `internal/provision/provision.go:399-400` scrubs `repo1-cipher-pass`, `repo1-s3-key-secret`, `repo1-s3-key` from provision exec output. Intact (gap N10 is only the *backup* package's separate execOK).
5. **Internal-error masking.** `internal/api/respond.go:36-38` masks `>=500` bodies; a failed pgcat-admin/pgx connect maps to `KindInternal`→500→"internal error", so the admin DSN never leaks in pool-stats responses. Intact.
6. **Derived secrets are sound.** `internal/provision/cipher.go` — `deriveCipherPass`/`RouterAdminPass` are HMAC-SHA256(masterKey, domain-prefix+id), per-instance/cluster, empty-key guarded; the master key is never logged. No weakness found.
7. **Async drain + WS hub.** `internal/api/async.go` (Add-under-lock vs closed, panic recovery) and `internal/ws/hub.go` (non-blocking Publish, locked map, idempotent cancel) — no races or goroutine leaks found.

---

## Fix first (CRIT/HIGH, High confidence)

1. **N1 — encrypted-repo break on flag toggle.** `internal/provision/provision.go:336` — persist per-instance encryption state; never derive the cipher line from a live global env flag. One control-plane restart with the flag changed can render encrypted instances unrecoverable.
2. **N2 — orphan catalog rows after backup delete.** `internal/backup/backup.go:171` — replace the single-label `catalog.Delete` with a full `s.sync` after `expire --set`, so cascade-expired dependents are pruned and the UI never shows phantom restore points.
3. **N5 — replica direct-destroy leaks the replication slot.** `internal/api/instances.go:308` — extend the destroy guard to all clustered members (or drop the slot in instance Destroy when `ClusterID` is set). Bounded by the 10 GB WAL cap, but still wrong.
4. **N3 — PgCat pool-mode/mirroring are dead config.** `internal/provision/router.go:126,128` — either wire them from cluster input (and persist for failover) or remove the fields; today the advertised feature does nothing.
5. **N4 — backup annotations never persisted/read back.** `internal/backup/catalog.go:23` — add the column + migration so the named-backup feature works end to end.

Then the defense-in-depth/robustness pass: N6 (remote-host SSRF guard), N7 (server-side identifier quoting / fix the false invariant), N9 (validate `REPO2_PATH`), N10 (redact backup execOK output), N11 (validate tab hash).

---

## Resolution (this pass) — 12 fixed, 2 accepted

Each fix was test-first; full Go suite + golangci-lint + web build/vitest green.

| ID | Sev | Fix | Where |
|----|-----|-----|-------|
| N1 | HIGH | Per-instance encryption is persisted (`instances.encrypted`, migration 00018) and stamped at stanza-create; the conf is derived from it, never the live global flag. Toggling the flag can no longer strip the cipher from an encrypted instance. | `instance` struct+repo, `provision.go`, migration 00018; tests incl. `TestBackrestConfEncryptionIgnoresGlobalFlagToggle` |
| N2 | HIGH | Single-backup delete now re-`sync`s the catalog after `expire --set` (Upsert survivors + Prune the cascade) instead of deleting one row — no phantom restore points. | `backup.go` Delete; updated `TestDeleteExpiresSetAndPrunesCatalog` |
| N3 | MED | PgCat pool mode wired end-to-end: `clusters.pool_mode` (migration 00019) ← cluster-create API + UI selector → router config; validated transaction\|session. No longer dead config. | `cluster`, `clusterctl`, `api/clusters.go`, `clusters/new` UI, migration 00019 |
| N4 | MED | Annotations persisted to `backups.annotations` (JSONB, migration 00017), loaded in List, shown as a badge on each backup row. | `catalog.go`, migration 00017, instance UI |
| N5 | MED | Destroy guard extended to ALL cluster members (replica direct-destroy refused) — no leaked replication slot. | `api/instances.go`; `TestDestroyClusterReplicaIsRefused` |
| N6 | MED | Remote-backup host rejects link-local/metadata IP literals (169.254.x / fe80::); private hosts still allowed. | `remotebackup.go` Validate; `TestRemoteConnValidateBlocksMetadata` |
| N7 | MED | Fixed the false "quoted server-side" comment; identifiers in the Create-DB/Role flows are now double-quote-escaped client-side too. | instance UI |
| N8 | MED | Failover re-applies the cluster's persisted `pool_mode` when repointing the router (no silent revert). | `failover.go` repointRouter |
| N9 | LOW | `PGFLEET_REPO2_PATH` validated at load (absolute, trimmed) — fail fast. | `config.go`; `validateRepo2Path` |
| N10 | LOW | Backup-package `execOK` runs failure output through a secret redactor (cipher pass / S3 keys). | `backup.go` |
| N11 | LOW | Tab deep-link hash validated against known tab ids; unknown → Overview. | instance UI |
| N14 | LOW | Backup annotation trimmed, control-chars stripped, length-bounded (200). | `backup.go` `sanitizeAnnotation` |

**Accepted (documented, not bugs to fix now):**
- **N12** — `/auth/sso` trusts the proxy header by design (forward-auth); the deployment invariant (strip client copies, API only reachable via the proxy) is documented in `authelia-sso.md`.
- **N13** — `/events` WS is fleet-wide; PgFleet is single-org, so per-resource scoping isn't needed yet. (Mirroring config is supported by `pgcat.Generate` but intentionally not surfaced in the UI yet — documented in `router.go`.)
