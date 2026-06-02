-- +goose Up
-- Track each instance's current data volume so restore can swap onto a fresh
-- volume (instead of mutating the only copy of the data in place).
ALTER TABLE instances ADD COLUMN data_volume TEXT NOT NULL DEFAULT '';
UPDATE instances SET data_volume = 'pgfleet-data-' || id::text WHERE data_volume = '';

-- +goose Down
ALTER TABLE instances DROP COLUMN data_volume;
