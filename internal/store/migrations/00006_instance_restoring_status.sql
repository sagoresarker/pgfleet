-- +goose Up
ALTER TABLE instances DROP CONSTRAINT instances_status_check;
ALTER TABLE instances ADD CONSTRAINT instances_status_check
    CHECK (status IN ('provisioning', 'running', 'stopped', 'error', 'destroying', 'restoring'));

-- +goose Down
-- Normalize any in-flight 'restoring' rows so re-adding the stricter
-- constraint does not fail validation against existing data.
UPDATE instances SET status = 'error', last_error = 'restore interrupted by rollback'
    WHERE status = 'restoring';
ALTER TABLE instances DROP CONSTRAINT instances_status_check;
ALTER TABLE instances ADD CONSTRAINT instances_status_check
    CHECK (status IN ('provisioning', 'running', 'stopped', 'error', 'destroying'));
