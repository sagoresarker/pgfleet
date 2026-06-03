-- +goose Up
-- Enforce at most one primary per cluster so a failover/provisioning bug can't
-- leave split-brain metadata.
CREATE UNIQUE INDEX one_primary_per_cluster ON instances (cluster_id)
    WHERE role = 'primary' AND cluster_id IS NOT NULL;

-- Keep clusters.primary_instance_id referentially valid; null it if the primary
-- instance row is removed.
ALTER TABLE clusters ADD CONSTRAINT clusters_primary_fk
    FOREIGN KEY (primary_instance_id) REFERENCES instances(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE clusters DROP CONSTRAINT clusters_primary_fk;
DROP INDEX one_primary_per_cluster;
