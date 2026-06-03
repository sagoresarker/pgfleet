-- +goose Up
-- Per-instance at-rest backup encryption state, stamped at stanza-create time.
-- pgBackRest fixes the repo cipher at stanza-create and it cannot be changed
-- afterward, so the conf for an instance must always reflect the value it was
-- created with — never a live global env flag (which, if toggled, would make an
-- encrypted repo unrecoverable). false for all existing (unencrypted) instances.
ALTER TABLE instances ADD COLUMN encrypted BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE instances DROP COLUMN encrypted;
