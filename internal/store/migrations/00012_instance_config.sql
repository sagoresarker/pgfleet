-- +goose Up
-- User-supplied Postgres tuning: GUC parameters (validated, platform-owned keys
-- rejected) and a curated set of extensions to CREATE at provision time.
ALTER TABLE instances
    ADD COLUMN parameters JSONB    NOT NULL DEFAULT '{}',
    ADD COLUMN extensions TEXT[]   NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE instances
    DROP COLUMN parameters,
    DROP COLUMN extensions;
