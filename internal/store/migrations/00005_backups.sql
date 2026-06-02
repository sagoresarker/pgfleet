-- +goose Up
CREATE TABLE backups (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id  UUID        NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    label        TEXT        NOT NULL,
    type         TEXT        NOT NULL,
    repo_size    BIGINT      NOT NULL DEFAULT 0,
    logical_size BIGINT      NOT NULL DEFAULT 0,
    wal_start    TEXT        NOT NULL DEFAULT '',
    wal_stop     TEXT        NOT NULL DEFAULT '',
    started_at   TIMESTAMPTZ,
    stopped_at   TIMESTAMPTZ,
    error        BOOLEAN     NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT backups_instance_label_uniq UNIQUE (instance_id, label)
);

CREATE INDEX backups_instance_idx ON backups (instance_id, stopped_at DESC);

-- +goose Down
DROP TABLE backups;
