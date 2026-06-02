-- +goose Up
CREATE TABLE metric_samples (
    instance_id UUID             NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    metric      TEXT             NOT NULL,
    value       DOUBLE PRECISION NOT NULL,
    at          TIMESTAMPTZ      NOT NULL DEFAULT now()
);

CREATE INDEX metric_samples_lookup_idx ON metric_samples (instance_id, metric, at DESC);

-- +goose Down
DROP TABLE metric_samples;
