-- +goose Up
-- Per-instance network exposure. false (default) binds the published port to
-- the secure address (127.0.0.1); true binds to all interfaces (0.0.0.0).
ALTER TABLE instances ADD COLUMN public BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE instances DROP COLUMN public;
