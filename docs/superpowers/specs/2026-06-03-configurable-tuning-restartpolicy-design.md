# Increment 1 — Configurable Postgres tuning + container RestartPolicy

Status: approved 2026-06-03. First increment of the configurability/observability/
durability roadmap. Bundles roadmap **Phase 4.1** (RestartPolicy) with **Area 1**
(generic GUC + extension configuration), since both touch the provisioning path and
Area 1 is the foundation for Areas 2 and 3.4.

## Approved decisions

- **Extensions:** curated allowlist (not free-form).
- **Config timing:** provision-time only for v1 (no post-provision live update).
- **Cluster config:** per-cluster — one config applied identically to every member.
- **Platform-owned GUC enforcement:** hard reject (400) naming the offending key.
- **RestartPolicy default:** `unless-stopped` (configurable via env), on instances and
  cluster routers. Recovers crashes/host reboots; leaves a deliberately-stopped
  instance stopped.
- **GUC values:** name-format validated + platform-key rejected; Postgres validates the
  value itself at startup (a bad value surfaces as a provision error).

## Global constraints honored

Platform-owned GUCs (`archive_mode`, `archive_command`, `wal_level`, `max_wal_senders`,
`max_replication_slots`, `max_slot_wal_keep_size`, `hot_standby`,
`shared_preload_libraries`) cannot be overridden. `shared_preload_libraries` is a MERGE:
`pg_stat_statements` + any preload libs the requested extensions need. WAL archiving stays
control-plane-independent. Safe-restore (staging-volume swap) is untouched. Static GUCs
require recreate — explicitly out of scope to change post-provision in v1.

## Phase 4.1 — RestartPolicy

- `internal/docker`: `ContainerSpec.RestartPolicy string` + `ContainerState.RestartPolicy
  string`. `moby.go` sets `HostConfig.RestartPolicy` on create and maps it back on Inspect;
  the in-memory `Fake` records and returns it.
- `internal/provision` `containerSpec()` and `internal/provision/router.go` `StartRouter()`
  set the policy from `Options.RestartPolicy`.
- `internal/config`: `PGFLEET_INSTANCE_RESTART_POLICY` (default `unless-stopped`), wired into
  provision `Options` in `cmd/pgfleet-api/main.go`.
- No reconciler change: the policy handles crash/reboot recovery; a deliberate `Stop`
  remains authoritative (Docker does not restart an explicitly-stopped container under
  `unless-stopped`).
- Tests: unit — provision/router set the policy (Fake Inspect). Integration — provision →
  `docker kill` → container returns running on the same volume with no control-plane action.

## Area 1 — Configurable tuning

### Schema
`internal/store/migrations/00012_instance_config.sql`: add to `instances`
`parameters JSONB NOT NULL DEFAULT '{}'` and `extensions TEXT[] NOT NULL DEFAULT '{}'`,
with a down-migration dropping both. No `clusters` column — cluster config is written to
each member's instance row (all identical).

### Validator — `internal/pgconfig` (single security boundary)
- `ValidateParameters(map[string]string) error` — reject platform-owned keys; key matches
  `^[a-z][a-z0-9_]*$`; value non-empty, no newline/NUL.
- `ValidateExtensions([]string) error` — each must be in the allowlist
  `{pg_trgm, pgcrypto, uuid-ossp, hstore, citext}` (contrib, present in the base image;
  `timescaledb` is added by Area 2).
- `PreloadLibraries(extensions []string) []string` — `pg_stat_statements` merged with the
  preload libs required by the requested extensions (none of the v1 allowlist needs preload;
  the merge machinery is in place for Area 2's timescaledb).

### Domain / provisioning
- `instance.Instance` and `instance.NewInstance` gain `Parameters map[string]string` and
  `Extensions []string`; the repository persists/scans the JSONB + text[].
- `provision.containerSpec()` appends validated `-c key=value` after the platform flags and
  sets `shared_preload_libraries` from `PreloadLibraries(extensions)`.
- After `pg_isready`, run `CREATE EXTENSION IF NOT EXISTS <name>` per requested extension
  (mirrors the existing `pg_stat_statements` creation).

### API
- `createInstanceRequest` and `createClusterRequest` gain optional `parameters` +
  `extensions`; validated (400 on bad input) and plumbed through. `clusterctl.Input` carries
  them and sets them on every member spec. `instancePayload` returns them (read-only).

### Frontend
- A collapsible "Advanced · Postgres tuning" section on the instance and cluster create
  forms: key/value parameter rows + extension multi-select (allowlist). Read-only display on
  the instance detail page.

### Testing (TDD)
- Unit: validator rejects every platform key, bad key format, bad value, non-allowlist
  extension; accepts valid input; preload merge always includes `pg_stat_statements`.
- Integration: provision with `work_mem=8MB` + `pg_trgm` → assert `SHOW work_mem` and
  extension existence and that `pgbackrest check` still passes; cluster → config reaches every
  member and replication still works.

## Out of scope (explicit follow-ups)
Post-provision config changes (live `ALTER SYSTEM` / recreate), per-member overrides,
timescaledb (Area 2), and everything in Areas 2–4.
