# Disaster recovery — recovering without PgFleet

PgFleet is **removable from the data path**. Your databases and their backups do
not depend on the control plane being up:

- **WAL archiving runs inside each Postgres container** (`archive_command =
  pgbackrest ... archive-push`), so WAL keeps flowing to the repo even if the
  control plane is down.
- **Instance data lives in named Docker volumes** that survive restarts, and
  containers carry `RestartPolicy=unless-stopped` so they return after a host
  reboot on their own.
- **Backups live in the pgBackRest repo** (S3/MinIO or a local volume),
  independent of the meta DB.

This runbook restores data using only the repo + the managed image — **no API,
no meta DB**. It also covers recovering the control plane's own state.

> **Production recommendation:** use an **external, immutable** object store for
> the repo — S3 with **versioning + Object Lock** — on different hardware than
> your data. A local-volume repo on the same host as the data means one disk
> failure loses both. PgFleet's UI flags clusters using local-only backups.

---

## 0. What you need

- The backup repo credentials + the **stanza name** (the stanza == the instance
  name; it is also stamped on the container as the `pgfleet.stanza` label).
- The managed image `pgfleet/postgres-pgbackrest:<ver>` (it bundles `pgbackrest`,
  `pg_ctl`, `gosu`).
- A Docker daemon.

Every managed container is labelled with non-secret recovery metadata —
inspect them to rediscover your fleet if the meta DB is gone:

```bash
docker ps -a --filter "label=pgfleet.managed=true" \
  --format '{{.Names}}\t{{.Label "pgfleet.name"}}\t{{.Label "pgfleet.stanza"}}\t{{.Label "pgfleet.role"}}\t{{.Label "pgfleet.repo"}}'
```

---

## 1. Restore an instance from the repo — with the `pgfleet` CLI

```bash
# Build the standalone DR binary (no control plane needed to run it):
make cli   # -> ./bin/pgfleet

./bin/pgfleet restore \
  --stanza orders-db \
  --repo-type s3 \
  --s3-endpoint s3.amazonaws.com --s3-bucket my-pgbackrest --s3-region us-east-1 \
  --s3-key "$AWS_ACCESS_KEY_ID" --s3-secret "$AWS_SECRET_ACCESS_KEY" \
  --data-volume orders-db-recovered \
  --type time --target "2026-06-03 12:00:00+00"   # omit --type/--target for latest
```

It restores into a fresh Docker volume and prints the `docker run` command to
start Postgres on it. PITR: pass `--type time --target <timestamp>`.

## 1b. …or with raw pgbackrest (no PgFleet code at all)

If you don't trust any PgFleet binary, the repo is restorable with stock tools.
Write a `pgbackrest.conf` for the stanza (S3 example):

```ini
[global]
repo1-type=s3
repo1-s3-endpoint=s3.amazonaws.com
repo1-s3-bucket=my-pgbackrest
repo1-s3-region=us-east-1
repo1-s3-key=...
repo1-s3-key-secret=...
repo1-s3-uri-style=path
repo1-path=/stanzas/orders-db

[orders-db]
pg1-path=/var/lib/postgresql/data
```

Then restore into a volume using the managed image:

```bash
docker volume create orders-db-recovered
docker run --rm -v orders-db-recovered:/var/lib/postgresql/data \
  -v "$PWD/pgbackrest.conf:/etc/pgbackrest/pgbackrest.conf" \
  pgfleet/postgres-pgbackrest:16 bash -c '
    chown -R postgres:postgres /var/lib/postgresql/data &&
    gosu postgres pgbackrest --config=/etc/pgbackrest/pgbackrest.conf --stanza=orders-db \
       --type=time --target="2026-06-03 12:00:00+00" --target-action=promote restore &&
    gosu postgres pg_ctl -D /var/lib/postgresql/data -w start &&
    gosu postgres pg_ctl -D /var/lib/postgresql/data -m fast stop'

docker run -d --name orders-db-recovered -p 127.0.0.1:5432:5432 \
  -v orders-db-recovered:/var/lib/postgresql/data pgfleet/postgres-pgbackrest:16
```

`pgbackrest info --stanza=orders-db` lists available backups + the recoverable
WAL range.

---

## 2. Recover the control-plane state (meta DB)

The control plane dumps its own meta DB to the object store every 6h
(`meta-backups/pgfleet-meta-<stamp>.dump`). After a meta-DB loss, restore it:

```bash
./bin/pgfleet meta-restore \
  --dsn "postgres://pgfleet:pgfleet@localhost:5433/pgfleet?sslmode=disable" \
  --s3-endpoint s3.amazonaws.com --s3-bucket my-pgbackrest --s3-region us-east-1 \
  --s3-key "$AWS_ACCESS_KEY_ID" --s3-secret "$AWS_SECRET_ACCESS_KEY"
  # add --object meta-backups/pgfleet-meta-<stamp>.dump to pick a specific dump;
  # default is the newest.
```

Then start the control plane against the restored meta DB; the reconciler
re-adopts the still-running instance containers.

**If you have no meta backup**, you can still operate: the instance data +
backups are intact (sections 1/1b), and the container labels (section 0) tell
you each instance's name, stanza, role, repo, and cluster — enough to
re-provision metadata or restore each instance directly. The only thing not
recoverable from Docker alone is the **encrypted superuser password** (it lived
only in the meta DB) — set a new one after restore.

---

## 3. RPO / RTO

- **RPO:** with `archive_timeout=60`, total-loss recovery can lose up to ~60s of
  WAL. Lower `archive_timeout` (a tuning parameter) to tighten it.
- **RTO:** dominated by the restore (full backup size + WAL replay). Take more
  frequent backups and use `--type time` to bound replay.
- **Prove it:** PgFleet runs an automated restore drill, but you should also run
  section 1 into a throwaway volume and diff the data — an untested backup is
  not a backup.
