// Package cluster models high-availability Postgres clusters: a primary, its
// streaming-replication read replicas, and a query router (PgCat) that fronts
// them.
package cluster

import (
	"regexp"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Status is the lifecycle state of a cluster.
type Status string

const (
	StatusProvisioning Status = "provisioning"
	StatusRunning      Status = "running"
	StatusDegraded     Status = "degraded" // up but a member is unhealthy
	StatusError        Status = "error"
	StatusDestroying   Status = "destroying"
)

// nameRe matches the same DNS-safe form as instance names.
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}$`)

// Cluster groups a primary + replicas behind a router.
type Cluster struct {
	ID                string
	Name              string
	Status            Status
	PrimaryInstanceID string // "" until the primary is provisioned
	RouterContainerID string
	RouterPort        int
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// NewCluster is the input for creating a cluster.
type NewCluster struct {
	Name string
}

// Validate checks the create input.
func (n NewCluster) Validate() error {
	if !nameRe.MatchString(n.Name) {
		return apperr.New(apperr.KindInvalid, "cluster: name must match [a-z][a-z0-9-]{1,38}")
	}
	return nil
}
