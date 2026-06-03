// Package pgbackrest generates pgBackRest / PostgreSQL configuration and builds
// and parses pgBackRest commands. Configuration and command building are pure
// functions so they can be golden-tested without containers.
package pgbackrest

import (
	"fmt"
	"strings"
)

// RepoS3 holds S3/MinIO repository settings.
type RepoS3 struct {
	Endpoint   string
	Bucket     string
	Region     string
	Key        string
	Secret     string
	PathPrefix string // per-instance prefix, e.g. /stanzas/<name>
	URIStyle   string // "path" (default, required for MinIO) or "host"
	VerifyTLS  bool
}

// RepoLocal holds posix (local volume) repository settings.
type RepoLocal struct {
	Path string
}

// InstanceConf is the input for generating an instance's pgbackrest.conf.
type InstanceConf struct {
	Stanza        string
	PGDataPath    string
	PGPort        int
	RetentionFull int
	RepoType      string // "s3" | "local"
	S3            RepoS3
	Local         RepoLocal
	// CipherPass, when non-empty, enables at-rest repository encryption
	// (aes-256-cbc) for this stanza's backups and WAL. It MUST be the same value
	// for the life of the stanza: pgBackRest fixes the cipher at stanza-create
	// time, and every subsequent command (backup, archive-push, restore) must
	// supply the identical passphrase or it cannot read the repo. Empty leaves
	// encryption off (repo1-cipher-type defaults to none).
	CipherPass string
}

// BackrestConf renders a complete pgbackrest.conf for an instance.
func BackrestConf(c InstanceConf) (string, error) {
	retention := c.RetentionFull
	if retention <= 0 {
		retention = 2
	}

	var b strings.Builder
	b.WriteString("[global]\n")

	switch c.RepoType {
	case "s3":
		uriStyle := c.S3.URIStyle
		if uriStyle == "" {
			uriStyle = "path"
		}
		verify := "n"
		if c.S3.VerifyTLS {
			verify = "y"
		}
		fmt.Fprintf(&b, "repo1-type=s3\n")
		fmt.Fprintf(&b, "repo1-path=%s\n", c.S3.PathPrefix)
		fmt.Fprintf(&b, "repo1-s3-endpoint=%s\n", c.S3.Endpoint)
		fmt.Fprintf(&b, "repo1-s3-bucket=%s\n", c.S3.Bucket)
		fmt.Fprintf(&b, "repo1-s3-region=%s\n", c.S3.Region)
		fmt.Fprintf(&b, "repo1-s3-uri-style=%s\n", uriStyle)
		fmt.Fprintf(&b, "repo1-s3-key=%s\n", c.S3.Key)
		fmt.Fprintf(&b, "repo1-s3-key-secret=%s\n", c.S3.Secret)
		fmt.Fprintf(&b, "repo1-storage-verify-tls=%s\n", verify)
	case "local":
		fmt.Fprintf(&b, "repo1-type=posix\n")
		fmt.Fprintf(&b, "repo1-path=%s\n", c.Local.Path)
	default:
		return "", fmt.Errorf("pgbackrest: unknown repo type %q", c.RepoType)
	}

	// At-rest repository encryption. Emitted for every repo type when a cipher
	// pass is supplied; absent (cipher-type defaults to none) when it is not.
	// This is set at stanza-create and CANNOT be added to an existing stanza, so
	// the caller only sets CipherPass for newly provisioned instances.
	if c.CipherPass != "" {
		fmt.Fprintf(&b, "repo1-cipher-type=aes-256-cbc\n")
		fmt.Fprintf(&b, "repo1-cipher-pass=%s\n", c.CipherPass)
	}

	fmt.Fprintf(&b, "repo1-retention-full=%d\n", retention)
	b.WriteString("start-fast=y\n")
	b.WriteString("log-level-console=info\n")

	b.WriteString("\n")
	fmt.Fprintf(&b, "[%s]\n", c.Stanza)
	fmt.Fprintf(&b, "pg1-path=%s\n", c.PGDataPath)
	fmt.Fprintf(&b, "pg1-port=%d\n", c.PGPort)

	return b.String(), nil
}

// PostgresConf renders the PostgreSQL settings that enable WAL archiving to
// pgBackRest for the given stanza. archive_mode requires a full restart, so
// these must be present before the cluster first starts.
func PostgresConf(stanza string) string {
	var b strings.Builder
	b.WriteString("archive_mode = on\n")
	b.WriteString("archive_command = 'pgbackrest --stanza=" + stanza + " archive-push %p'\n")
	b.WriteString("archive_timeout = 60\n")
	b.WriteString("wal_level = replica\n")
	b.WriteString("max_wal_senders = 3\n")
	return b.String()
}
