-- +goose Up
CREATE TABLE instances (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT        NOT NULL UNIQUE,
    status           TEXT        NOT NULL DEFAULT 'provisioning',
    image            TEXT        NOT NULL,
    pg_version       TEXT        NOT NULL DEFAULT '16',
    container_id     TEXT        NOT NULL DEFAULT '',
    host_port        INTEGER     NOT NULL DEFAULT 0,
    repo_type        TEXT        NOT NULL DEFAULT 's3',
    stanza           TEXT        NOT NULL,
    superuser        TEXT        NOT NULL DEFAULT 'postgres',
    superuser_secret BYTEA,
    last_error       TEXT        NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT instances_status_check
        CHECK (status IN ('provisioning', 'running', 'stopped', 'error', 'destroying')),
    CONSTRAINT instances_repo_type_check
        CHECK (repo_type IN ('s3', 'local'))
);

CREATE INDEX instances_status_idx ON instances (status);

-- +goose Down
DROP TABLE instances;
