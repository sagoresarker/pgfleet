// Package instance models managed Postgres instances and their lifecycle.
package instance

import (
	"regexp"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Status is the lifecycle state of a managed instance.
type Status string

const (
	StatusProvisioning Status = "provisioning"
	StatusRunning      Status = "running"
	StatusStopped      Status = "stopped"
	StatusError        Status = "error"
	StatusDestroying   Status = "destroying"
	StatusRestoring    Status = "restoring"
)

// Role is an instance's role within a cluster.
type Role string

const (
	RoleStandalone Role = "standalone"
	RolePrimary    Role = "primary"
	RoleReplica    Role = "replica"
)

// RepoType selects where pgBackRest stores backups.
type RepoType string

const (
	RepoS3    RepoType = "s3"
	RepoLocal RepoType = "local"
)

// Valid reports whether r is a known repository type.
func (r RepoType) Valid() bool { return r == RepoS3 || r == RepoLocal }

// SupportedVersions are the PostgreSQL major versions PgFleet can provision,
// newest first. Each needs a built pgfleet/postgres-pgbackrest:<v> image
// (see `make images`).
var SupportedVersions = []string{"17", "16", "15", "14", "13"}

// DefaultVersion is used when a create request omits the version. It is kept at
// 16 so the default matches the image `make image` builds.
const DefaultVersion = "16"

// VersionSupported reports whether v is a supported major version.
func VersionSupported(v string) bool {
	for _, s := range SupportedVersions {
		if s == v {
			return true
		}
	}
	return false
}

// ImageForVersion returns the managed-instance image tag for a major version.
func ImageForVersion(v string) string {
	return "pgfleet/postgres-pgbackrest:" + v
}

// minInstancePasswordLen bounds the superuser password length.
const minInstancePasswordLen = 8

// nameRe restricts instance names to a DNS-label-like, pgBackRest-safe form:
// lowercase, starts with a letter, alphanumeric or hyphen, 2-39 chars.
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}$`)

// Instance is a managed Postgres instance. The superuser password is never
// part of this struct; retrieve it via Repository.Password.
type Instance struct {
	ID          string
	Name        string
	Status      Status
	Image       string
	PGVersion   string
	ContainerID string
	HostPort    int
	DataVolume  string
	RepoType    RepoType
	Stanza      string
	Superuser   string
	LastError   string
	ClusterID   string // "" for standalone instances
	Role        Role
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewInstance is the input for provisioning an instance.
type NewInstance struct {
	Name      string
	Image     string
	PGVersion string
	ClusterID string // optional; "" for standalone
	Role      Role   // defaults to standalone
	RepoType  RepoType
	Superuser string
	Password  string
}

// Validate checks the create input.
func (n NewInstance) Validate() error {
	if !nameRe.MatchString(n.Name) {
		return apperr.New(apperr.KindInvalid, "instance: name must match [a-z][a-z0-9-]{1,38}")
	}
	if !n.RepoType.Valid() {
		return apperr.New(apperr.KindInvalid, "instance: invalid repo type")
	}
	if len(n.Password) < minInstancePasswordLen {
		return apperr.New(apperr.KindInvalid, "instance: superuser password too short")
	}
	// An empty version is filled in with DefaultVersion later; a non-empty one
	// must be a supported major version.
	if n.PGVersion != "" && !VersionSupported(n.PGVersion) {
		return apperr.New(apperr.KindInvalid, "instance: unsupported Postgres version")
	}
	return nil
}

// StanzaFor returns the deterministic pgBackRest stanza name for an instance.
func StanzaFor(name string) string { return name }
