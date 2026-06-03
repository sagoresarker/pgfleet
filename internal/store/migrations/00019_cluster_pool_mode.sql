-- +goose Up
-- PgCat pooling mode for a cluster's router (transaction | session). Persisted
-- so it survives a control-plane restart and is re-applied when failover
-- repoints the router to a new primary.
ALTER TABLE clusters ADD COLUMN pool_mode TEXT NOT NULL DEFAULT 'transaction';

-- +goose Down
ALTER TABLE clusters DROP COLUMN pool_mode;
