# PgFleet managed-instance image: PostgreSQL + pgBackRest in one image so
# pgBackRest can run archive-push/backup/restore against the local cluster and
# an S3/MinIO or local-volume repository. pgBackRest runs as the postgres OS
# user (uid 999 in the official image) so it shares ownership of PGDATA/pg_wal.
#
# PG_VERSION selects the PostgreSQL major version (13–17). Build a specific
# version with: docker build --build-arg PG_VERSION=17 -t pgfleet/postgres-pgbackrest:17 .
ARG PG_VERSION=16
FROM postgres:${PG_VERSION}

# pgbackrest from the Debian Bookworm repos. Pin nothing here; the integration
# build records the resolved version. Create the directories pgBackRest needs,
# owned by the postgres user.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends pgbackrest ca-certificates; \
    rm -rf /var/lib/apt/lists/*; \
    mkdir -p /etc/pgbackrest /var/lib/pgbackrest /var/log/pgbackrest /var/spool/pgbackrest; \
    chown -R postgres:postgres /etc/pgbackrest /var/lib/pgbackrest /var/log/pgbackrest /var/spool/pgbackrest; \
    pgbackrest version

LABEL org.pgfleet.image="postgres-pgbackrest"
