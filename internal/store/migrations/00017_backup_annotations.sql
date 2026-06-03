-- +goose Up
-- User-supplied annotations stored on a pgBackRest backup set (e.g. a "name"
-- or note). Mirrored from `pgbackrest info` so the catalog can show them.
ALTER TABLE backups ADD COLUMN annotations JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE backups DROP COLUMN annotations;
