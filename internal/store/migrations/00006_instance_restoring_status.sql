-- +goose Up
ALTER TABLE instances DROP CONSTRAINT instances_status_check;
ALTER TABLE instances ADD CONSTRAINT instances_status_check
    CHECK (status IN ('provisioning', 'running', 'stopped', 'error', 'destroying', 'restoring'));

-- +goose Down
ALTER TABLE instances DROP CONSTRAINT instances_status_check;
ALTER TABLE instances ADD CONSTRAINT instances_status_check
    CHECK (status IN ('provisioning', 'running', 'stopped', 'error', 'destroying'));
