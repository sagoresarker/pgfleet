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
