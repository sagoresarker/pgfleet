-- +goose Up
-- pgcrypto provides gen_random_uuid(), used as the default primary key across
-- the control-plane schema.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- +goose Down
DROP EXTENSION IF EXISTS pgcrypto;
