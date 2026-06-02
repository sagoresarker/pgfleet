-- +goose Up
CREATE TABLE audit_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor      TEXT        NOT NULL,
    action     TEXT        NOT NULL,
    target     TEXT        NOT NULL DEFAULT '',
    metadata   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_created_at_idx ON audit_log (created_at DESC);
CREATE INDEX audit_log_target_idx ON audit_log (target) WHERE target <> '';

-- +goose Down
DROP TABLE audit_log;
