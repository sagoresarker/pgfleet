package pgbackrest

import (
	"strings"
	"testing"
)

func TestBackrestConfS3(t *testing.T) {
	got, err := BackrestConf(InstanceConf{
		Stanza:        "orders-db",
		PGDataPath:    "/var/lib/postgresql/data",
		PGPort:        5432,
		RetentionFull: 2,
		RepoType:      "s3",
		S3: RepoS3{
			Endpoint:   "minio:9000",
			Bucket:     "pgbackrest",
			Region:     "us-east-1",
			Key:        "AKIA",
			Secret:     "s3cr3t",
			PathPrefix: "/stanzas/orders-db",
			URIStyle:   "path",
			VerifyTLS:  false,
		},
	})
	if err != nil {
		t.Fatalf("BackrestConf: %v", err)
	}

	want := strings.Join([]string{
		"[global]",
		"repo1-type=s3",
		"repo1-path=/stanzas/orders-db",
		"repo1-s3-endpoint=minio:9000",
		"repo1-s3-bucket=pgbackrest",
		"repo1-s3-region=us-east-1",
		"repo1-s3-uri-style=path",
		"repo1-s3-key=AKIA",
		"repo1-s3-key-secret=s3cr3t",
		"repo1-storage-verify-tls=n",
		"repo1-retention-full=2",
		"start-fast=y",
		"log-level-console=info",
		"",
		"[orders-db]",
		"pg1-path=/var/lib/postgresql/data",
		"pg1-port=5432",
		"",
	}, "\n")

	if got != want {
		t.Errorf("S3 conf mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBackrestConfS3DefaultsURIStyleToPath(t *testing.T) {
	// Omitting URIStyle must still emit path-style (the #1 MinIO gotcha).
	got, _ := BackrestConf(InstanceConf{
		Stanza: "db", PGDataPath: "/d", PGPort: 5432, RetentionFull: 1, RepoType: "s3",
		S3: RepoS3{Endpoint: "minio:9000", Bucket: "b", Region: "us-east-1", Key: "k", Secret: "s", PathPrefix: "/p"},
	})
	if !strings.Contains(got, "repo1-s3-uri-style=path") {
		t.Errorf("expected path-style by default:\n%s", got)
	}
}

func TestBackrestConfS3VerifyTLS(t *testing.T) {
	got, _ := BackrestConf(InstanceConf{
		Stanza: "db", PGDataPath: "/d", PGPort: 5432, RetentionFull: 1, RepoType: "s3",
		S3: RepoS3{Endpoint: "s3.amazonaws.com", Bucket: "b", Region: "us-east-1", Key: "k", Secret: "s", PathPrefix: "/p", VerifyTLS: true},
	})
	if !strings.Contains(got, "repo1-storage-verify-tls=y") {
		t.Errorf("expected verify-tls=y when VerifyTLS set:\n%s", got)
	}
}

func TestBackrestConfLocal(t *testing.T) {
	got, err := BackrestConf(InstanceConf{
		Stanza:        "orders-db",
		PGDataPath:    "/var/lib/postgresql/data",
		PGPort:        5432,
		RetentionFull: 3,
		RepoType:      "local",
		Local:         RepoLocal{Path: "/var/lib/pgbackrest"},
	})
	if err != nil {
		t.Fatalf("BackrestConf: %v", err)
	}
	want := strings.Join([]string{
		"[global]",
		"repo1-type=posix",
		"repo1-path=/var/lib/pgbackrest",
		"repo1-retention-full=3",
		"start-fast=y",
		"log-level-console=info",
		"",
		"[orders-db]",
		"pg1-path=/var/lib/postgresql/data",
		"pg1-port=5432",
		"",
	}, "\n")
	if got != want {
		t.Errorf("local conf mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBackrestConfCipherS3(t *testing.T) {
	// When CipherPass is set, both cipher lines appear immediately after the
	// repo block and before retention, for an s3 repo.
	got, err := BackrestConf(InstanceConf{
		Stanza:        "orders-db",
		PGDataPath:    "/var/lib/postgresql/data",
		PGPort:        5432,
		RetentionFull: 2,
		RepoType:      "s3",
		S3: RepoS3{
			Endpoint:   "minio:9000",
			Bucket:     "pgbackrest",
			Region:     "us-east-1",
			Key:        "AKIA",
			Secret:     "s3cr3t",
			PathPrefix: "/stanzas/orders-db",
			URIStyle:   "path",
		},
		CipherPass: "deadbeef",
	})
	if err != nil {
		t.Fatalf("BackrestConf: %v", err)
	}
	want := strings.Join([]string{
		"[global]",
		"repo1-type=s3",
		"repo1-path=/stanzas/orders-db",
		"repo1-s3-endpoint=minio:9000",
		"repo1-s3-bucket=pgbackrest",
		"repo1-s3-region=us-east-1",
		"repo1-s3-uri-style=path",
		"repo1-s3-key=AKIA",
		"repo1-s3-key-secret=s3cr3t",
		"repo1-storage-verify-tls=n",
		"repo1-cipher-type=aes-256-cbc",
		"repo1-cipher-pass=deadbeef",
		"repo1-retention-full=2",
		"start-fast=y",
		"log-level-console=info",
		"",
		"[orders-db]",
		"pg1-path=/var/lib/postgresql/data",
		"pg1-port=5432",
		"",
	}, "\n")
	if got != want {
		t.Errorf("S3 cipher conf mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBackrestConfRepo2InheritsCipher(t *testing.T) {
	// When at-rest encryption is on AND a second repo is configured, repo2 MUST
	// carry the same cipher as repo1 — otherwise its copy of every backup is
	// plaintext (a silent encryption breach).
	got, err := BackrestConf(InstanceConf{
		Stanza:        "orders-db",
		PGDataPath:    "/var/lib/postgresql/data",
		PGPort:        5432,
		RetentionFull: 2,
		RepoType:      "local",
		Local:         RepoLocal{Path: "/var/lib/pgbackrest"},
		Repo2Path:     "/mnt/repo2",
		CipherPass:    "deadbeef",
	})
	if err != nil {
		t.Fatalf("BackrestConf: %v", err)
	}
	for _, want := range []string{
		"repo2-type=posix",
		"repo2-path=/mnt/repo2",
		"repo2-cipher-type=aes-256-cbc",
		"repo2-cipher-pass=deadbeef",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("repo2 cipher conf missing %q in:\n%s", want, got)
		}
	}
}

func TestBackrestConfRepo2NoCipherWhenUnencrypted(t *testing.T) {
	// Without a cipher pass, repo2 must NOT emit cipher lines.
	got, err := BackrestConf(InstanceConf{
		Stanza:        "orders-db",
		PGDataPath:    "/var/lib/postgresql/data",
		PGPort:        5432,
		RetentionFull: 2,
		RepoType:      "local",
		Local:         RepoLocal{Path: "/var/lib/pgbackrest"},
		Repo2Path:     "/mnt/repo2",
	})
	if err != nil {
		t.Fatalf("BackrestConf: %v", err)
	}
	if strings.Contains(got, "repo2-cipher") {
		t.Errorf("repo2 cipher should be absent when unencrypted:\n%s", got)
	}
}

func TestBackrestConfCipherLocal(t *testing.T) {
	// The same two lines must appear for a local (posix) repo.
	got, err := BackrestConf(InstanceConf{
		Stanza:        "orders-db",
		PGDataPath:    "/var/lib/postgresql/data",
		PGPort:        5432,
		RetentionFull: 3,
		RepoType:      "local",
		Local:         RepoLocal{Path: "/var/lib/pgbackrest"},
		CipherPass:    "cafef00d",
	})
	if err != nil {
		t.Fatalf("BackrestConf: %v", err)
	}
	want := strings.Join([]string{
		"[global]",
		"repo1-type=posix",
		"repo1-path=/var/lib/pgbackrest",
		"repo1-cipher-type=aes-256-cbc",
		"repo1-cipher-pass=cafef00d",
		"repo1-retention-full=3",
		"start-fast=y",
		"log-level-console=info",
		"",
		"[orders-db]",
		"pg1-path=/var/lib/postgresql/data",
		"pg1-port=5432",
		"",
	}, "\n")
	if got != want {
		t.Errorf("local cipher conf mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBackrestConfNoCipherByDefault(t *testing.T) {
	// With CipherPass empty, neither cipher line is emitted (cipher-type defaults
	// to none) for either repo type.
	for _, c := range []InstanceConf{
		{Stanza: "db", PGDataPath: "/d", PGPort: 5432, RepoType: "local", Local: RepoLocal{Path: "/r"}},
		{Stanza: "db", PGDataPath: "/d", PGPort: 5432, RepoType: "s3",
			S3: RepoS3{Endpoint: "minio:9000", Bucket: "b", Region: "us-east-1", Key: "k", Secret: "s", PathPrefix: "/p"}},
	} {
		got, err := BackrestConf(c)
		if err != nil {
			t.Fatalf("BackrestConf(%s): %v", c.RepoType, err)
		}
		if strings.Contains(got, "repo1-cipher-type") || strings.Contains(got, "repo1-cipher-pass") {
			t.Errorf("expected no cipher lines for %s when CipherPass empty:\n%s", c.RepoType, got)
		}
	}
}

func TestBackrestConfBlockIncr(t *testing.T) {
	got, err := BackrestConf(InstanceConf{
		Stanza: "db", PGDataPath: "/d", PGPort: 5432, RetentionFull: 2, RepoType: "local",
		Local: RepoLocal{Path: "/var/lib/pgbackrest"}, BlockIncr: true,
	})
	if err != nil {
		t.Fatalf("BackrestConf: %v", err)
	}
	if !strings.Contains(got, "repo1-block=y") {
		t.Errorf("expected repo1-block=y when BlockIncr set:\n%s", got)
	}
}

func TestBackrestConfNoBlockIncrByDefault(t *testing.T) {
	got, _ := BackrestConf(InstanceConf{
		Stanza: "db", PGDataPath: "/d", PGPort: 5432, RepoType: "local",
		Local: RepoLocal{Path: "/r"},
	})
	if strings.Contains(got, "repo1-block") {
		t.Errorf("expected no repo1-block line when BlockIncr false:\n%s", got)
	}
}

func TestBackrestConfRepo2(t *testing.T) {
	got, err := BackrestConf(InstanceConf{
		Stanza:        "orders-db",
		PGDataPath:    "/var/lib/postgresql/data",
		PGPort:        5432,
		RetentionFull: 3,
		RepoType:      "local",
		Local:         RepoLocal{Path: "/var/lib/pgbackrest"},
		Repo2Path:     "/mnt/backups",
	})
	if err != nil {
		t.Fatalf("BackrestConf: %v", err)
	}
	want := strings.Join([]string{
		"[global]",
		"repo1-type=posix",
		"repo1-path=/var/lib/pgbackrest",
		"repo1-retention-full=3",
		"repo2-type=posix",
		"repo2-path=/mnt/backups",
		"repo2-retention-full=3",
		"start-fast=y",
		"log-level-console=info",
		"",
		"[orders-db]",
		"pg1-path=/var/lib/postgresql/data",
		"pg1-port=5432",
		"",
	}, "\n")
	if got != want {
		t.Errorf("repo2 conf mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBackrestConfNoRepo2ByDefault(t *testing.T) {
	got, _ := BackrestConf(InstanceConf{
		Stanza: "db", PGDataPath: "/d", PGPort: 5432, RepoType: "local",
		Local: RepoLocal{Path: "/r"},
	})
	if strings.Contains(got, "repo2-") {
		t.Errorf("expected no repo2 lines when Repo2Path empty:\n%s", got)
	}
}

func TestBackrestConfRejectsUnknownRepoType(t *testing.T) {
	if _, err := BackrestConf(InstanceConf{Stanza: "db", RepoType: "nfs"}); err == nil {
		t.Error("unknown repo type should error")
	}
}

func TestBackrestConfDefaultsRetention(t *testing.T) {
	got, _ := BackrestConf(InstanceConf{
		Stanza: "db", PGDataPath: "/d", PGPort: 5432, RepoType: "local",
		Local: RepoLocal{Path: "/r"},
	})
	if !strings.Contains(got, "repo1-retention-full=2") {
		t.Errorf("expected default retention 2:\n%s", got)
	}
}

func TestPostgresConf(t *testing.T) {
	got := PostgresConf("orders-db")
	for _, want := range []string{
		"archive_mode = on",
		"archive_command = 'pgbackrest --stanza=orders-db archive-push %p'",
		"archive_timeout = 60",
		"wal_level = replica",
		"max_wal_senders = 3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("PostgresConf missing %q:\n%s", want, got)
		}
	}
}
