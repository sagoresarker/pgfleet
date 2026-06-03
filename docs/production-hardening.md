# Production hardening

What PgFleet does for security & reliability, how to turn it on, and the
deploy-level steps for the things that live outside the app.

## Shipped & toggleable

### Encrypted backups (at rest)
Set `PGFLEET_BACKUP_ENCRYPTION=true`. New instances then provision their
pgBackRest repo with `repo1-cipher-type=aes-256-cbc`; the passphrase is derived
deterministically per-instance from the master key (`HMAC-SHA256(masterKey,
"pgbackrest-cipher:"+instanceID)`) — nothing extra is stored, and it's stable
across restarts so restores work.

> **Constraint:** pgBackRest sets the cipher at `stanza-create` and it **cannot**
> be retrofitted onto an existing unencrypted stanza. This only affects instances
> created after enabling the flag.

### Disk I/O metrics (IOPS / throughput)
The resource collector now emits `disk_read_bytes`, `disk_write_bytes`,
`disk_read_ops`, `disk_write_ops` (cumulative counters; the UI computes rates).
On Docker Desktop / some cgroup-v2 hosts the recursive blkio stats are empty — in
that case PgFleet emits *nothing* for these rather than fake zeros, so they simply
won't appear there.

### Failover quorum guard
The failover controller now refuses to promote unless it can reach a strict
majority of cluster members — defeating split-brain when the controller is on the
minority side of a network partition. A 3-node cluster needs both replicas
reachable to promote. **A 2-node cluster cannot truly avoid split-brain without an
external witness** — run 3 nodes for real HA.

### Backup assurance (already present)
Scheduled restore drills restore the latest backup into a throwaway container and
validate it (`pg_controldata`); the Reliability page shows the pass/fail per
instance. Use it as the source of truth for "is my backup actually restorable?".

### Other existing guards
Secrets at rest (AES-256-GCM envelope), argon2id + JWT + RBAC, audit log,
instances bound to `127.0.0.1` by default, alert webhook SSRF guard, nonce CSP on
the dashboard, optional Authelia/OIDC SSO with MFA + brute-force regulation.

## Deploy-level (outside the app)

### TLS on Postgres
PgFleet binds instances to loopback by default; for encrypted client traffic,
terminate TLS at the connection edge or enable Postgres `ssl=on`:
1. Mount a server cert/key into the instance container and set `ssl=on`,
   `ssl_cert_file`, `ssl_key_file` in `postgresql.conf` (via the tuning params).
2. Require it in `pg_hba.conf` with `hostssl ... scram-sha-256`.
3. For the dashboard/API, put them behind the bundled Caddy/Authelia stack
   (`deploy/authelia/`) which already terminates HTTPS.
*Automated per-instance cert lifecycle (issuance/rotation) is a roadmap item.*

### Immutable / ransomware-resistant backups
Make the object-store repo append-only so a compromised control plane can't erase
your backups:
- Enable **Object Lock (WORM)** on the MinIO/S3 bucket in compliance/governance
  mode with a retention window ≥ your backup retention.
- Give PgFleet's S3 credentials `PutObject`/`GetObject` but **not**
  `DeleteObject`/`BypassGovernanceRetention`; let lifecycle policies expire old
  objects instead of the app deleting them.
- Keep the bucket private; never expose it publicly.

### Meta-DB high availability
The control plane's own Postgres is a single point of failure. For production:
- Run it as a replicated pair (primary + standby) or a managed HA Postgres.
- Keep the dogfooded meta-DB self-backup on (already scheduled to the object
  store) and rehearse the documented restore in `docs/disaster-recovery.md`.
- The control plane is stateless beyond the meta-DB and reconciles the fleet from
  Docker labels on boot, so it can be run as 2+ replicas behind the proxy.

### Disk-full protection
PgFleet alerts on `pg_wal`/data-volume disk pressure. Operationally, also:
provision data and WAL on a volume with headroom, set
`max_slot_wal_keep_size` so a stuck replication slot can't fill the disk, and
alert early (the alert thresholds are configurable).
