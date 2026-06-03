-- +goose Up
CREATE TABLE alerts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id UUID             REFERENCES instances(id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL,
    severity    TEXT        NOT NULL,
    state       TEXT        NOT NULL,
    message     TEXT        NOT NULL,
    value       DOUBLE PRECISION,
    threshold   DOUBLE PRECISION,
    fired_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT alerts_severity_check
        CHECK (severity IN ('warning', 'critical')),
    CONSTRAINT alerts_state_check
        CHECK (state IN ('firing', 'resolved'))
);

-- At most one firing alert per (instance, kind).
CREATE UNIQUE INDEX alerts_active_idx
    ON alerts (instance_id, kind) WHERE state = 'firing';

CREATE INDEX alerts_instance_idx ON alerts (instance_id);

-- +goose Down
DROP TABLE alerts;
