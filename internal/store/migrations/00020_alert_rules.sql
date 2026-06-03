-- +goose Up
CREATE TABLE alert_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id UUID             REFERENCES instances(id) ON DELETE CASCADE,
    kind        TEXT             NOT NULL,
    threshold   DOUBLE PRECISION NOT NULL,
    severity    TEXT             NOT NULL,
    enabled     BOOLEAN          NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ      NOT NULL DEFAULT now(),
    CONSTRAINT alert_rules_severity_check
        CHECK (severity IN ('warning', 'critical'))
);

-- Effective-rule lookups filter on (instance_id, enabled).
CREATE INDEX alert_rules_instance_idx ON alert_rules (instance_id);

-- +goose Down
DROP TABLE alert_rules;
