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
)

// RepoType selects where pgBackRest stores backups.
type RepoType string

const (
	RepoS3    RepoType = "s3"
	RepoLocal RepoType = "local"
)

// Valid reports whether r is a known repository type.
func (r RepoType) Valid() bool { return r == RepoS3 || r == RepoLocal }

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
	RepoType    RepoType
	Stanza      string
	Superuser   string
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewInstance is the input for provisioning an instance.
type NewInstance struct {
	Name      string
	Image     string
	PGVersion string
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
	return nil
}

// StanzaFor returns the deterministic pgBackRest stanza name for an instance.
func StanzaFor(name string) string { return name }
