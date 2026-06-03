-- +goose Up
-- remote_dumps catalogs logical backups (pg_dump custom format) captured from a
-- REMOTE Postgres that PgFleet does not manage. The source host is stored
-- REDACTED and the password is NEVER stored here (handled out of band / sealed
-- in remote_source_secrets). One row per captured dump.
CREATE TABLE remote_dumps (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    object_key   TEXT        NOT NULL UNIQUE,
    source_host  TEXT        NOT NULL,           -- redacted, e.g. "[REDACTED].com"
    source_db    TEXT        NOT NULL,
    server_major INT         NOT NULL DEFAULT 0,
    size_bytes   BIGINT      NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX remote_dumps_created_idx ON remote_dumps (created_at DESC);

-- remote_source_secrets seals the connection password (and other reusable
-- connection metadata) for a captured remote source, so a re-capture can be
-- performed later without the operator re-entering credentials. The password is
-- encrypted with the control-plane secrets cipher (NEVER plaintext).
CREATE TABLE remote_source_secrets (
    dump_id         UUID PRIMARY KEY REFERENCES remote_dumps(id) ON DELETE CASCADE,
    host            TEXT  NOT NULL,
    port            INT   NOT NULL DEFAULT 5432,
    db_user         TEXT  NOT NULL,
    dbname          TEXT  NOT NULL,
    sslmode         TEXT  NOT NULL DEFAULT 'prefer',
    password_secret BYTEA NOT NULL              -- sealed via internal/secrets
);

-- +goose Down
DROP TABLE remote_source_secrets;
DROP TABLE remote_dumps;
