-- +goose Up
CREATE TABLE instance_health (
    instance_id           UUID PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    archiving_ok          BOOLEAN     NOT NULL DEFAULT false,
    has_backup            BOOLEAN     NOT NULL DEFAULT false,
    last_backup_age_secs  BIGINT      NOT NULL DEFAULT 0,
    wal_bytes             BIGINT      NOT NULL DEFAULT 0,
    drill_ran             BOOLEAN     NOT NULL DEFAULT false,
    drill_ok              BOOLEAN     NOT NULL DEFAULT false,
    issues                TEXT[]      NOT NULL DEFAULT '{}',
    checked_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE instance_health;
