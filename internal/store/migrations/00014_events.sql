-- +goose Up
-- Durable lifecycle/status/health-transition events. Mirrors the in-memory ws
-- hub but survives a control-plane restart so the timeline stays queryable.
CREATE TABLE events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id UUID             REFERENCES instances(id) ON DELETE CASCADE,
    cluster_id  UUID             REFERENCES clusters(id)  ON DELETE CASCADE,
    type        TEXT        NOT NULL,
    message     TEXT        NOT NULL,
    metadata    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX events_instance_idx ON events (instance_id, created_at DESC);
CREATE INDEX events_cluster_idx  ON events (cluster_id, created_at DESC);
CREATE INDEX events_type_idx     ON events (type);

-- +goose Down
DROP TABLE events;
