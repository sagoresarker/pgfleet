-- +goose Up
CREATE TABLE clusters (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT        NOT NULL UNIQUE,
    status              TEXT        NOT NULL DEFAULT 'provisioning',
    primary_instance_id UUID,
    router_container_id TEXT        NOT NULL DEFAULT '',
    router_port         INTEGER     NOT NULL DEFAULT 0,
    last_error          TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT clusters_status_check
        CHECK (status IN ('provisioning', 'running', 'degraded', 'error', 'destroying'))
);

ALTER TABLE instances
    ADD COLUMN cluster_id UUID REFERENCES clusters(id) ON DELETE CASCADE,
    ADD COLUMN role TEXT NOT NULL DEFAULT 'standalone';

ALTER TABLE instances ADD CONSTRAINT instances_role_check
    CHECK (role IN ('standalone', 'primary', 'replica'));

CREATE INDEX instances_cluster_idx ON instances (cluster_id);

-- +goose Down
ALTER TABLE instances DROP CONSTRAINT instances_role_check;
ALTER TABLE instances DROP COLUMN role;
ALTER TABLE instances DROP COLUMN cluster_id;
DROP TABLE clusters;
