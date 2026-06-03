# PgFleet managed-instance image: PostgreSQL + pgBackRest in one image so
# pgBackRest can run archive-push/backup/restore against the local cluster and
# an S3/MinIO or local-volume repository. pgBackRest runs as the postgres OS
# user (uid 999 in the official image) so it shares ownership of PGDATA/pg_wal.
#
# PG_VERSION selects the PostgreSQL major version (13–17). Build a specific
# version with: docker build --build-arg PG_VERSION=17 -t pgfleet/postgres-pgbackrest:17 .
ARG PG_VERSION=16
FROM postgres:${PG_VERSION}

# In RUN steps below we use the base image's own PG_MAJOR env (e.g. "16"), NOT
# our PG_VERSION ARG. The postgres image already sets ENV PG_VERSION to the full
# Debian package version (e.g. "16.14-1.pgdg13+1"), which shadows a same-named
# ARG in the legacy builder — so referencing ${PG_VERSION} in a RUN would build
# the wrong package name. PG_MAJOR is the clean major and is set correctly for
# whichever postgres:<ver> base FROM selected.

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

# TimescaleDB Community edition (Apache + Timescale License / TSL features). The
# extension's columnar compression, continuous aggregates, and policy jobs all
# live in this package. The extension is loaded at runtime via
# shared_preload_libraries (managed by PgFleet's pgconfig, not here), and we do
# NOT run timescaledb-tune — PgFleet owns the GUCs.
#
# TimescaleDB supports PostgreSQL 14–17 but NOT 13, so the install is made
# tolerant: the PG13 image still builds, just without the extension available.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends gnupg lsb-release ca-certificates wget; \
    echo "deb https://packagecloud.io/timescale/timescaledb/debian/ bookworm main" \
        > /etc/apt/sources.list.d/timescaledb.list; \
    wget --quiet -O - https://packagecloud.io/timescale/timescaledb/gpgkey \
        | gpg --dearmor -o /etc/apt/trusted.gpg.d/timescaledb.gpg; \
    apt-get update; \
    if [ "${PG_MAJOR}" = "13" ]; then \
        echo "timescaledb does not support PostgreSQL 13; skipping"; \
    else \
        apt-get install -y "timescaledb-2-postgresql-${PG_MAJOR}"; \
        test -f "/usr/lib/postgresql/${PG_MAJOR}/lib/timescaledb.so"; \
    fi; \
    rm -rf /var/lib/apt/lists/*

LABEL org.pgfleet.image="postgres-pgbackrest"
