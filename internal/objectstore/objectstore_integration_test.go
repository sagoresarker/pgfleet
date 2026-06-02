//go:build integration

package objectstore

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func minioConfig(t *testing.T, bucket string) Config {
	m := testsupport.StartMinIO(t)
	return Config{
		Endpoint:  m.Endpoint,
		Region:    "us-east-1",
		AccessKey: m.AccessKey,
		SecretKey: m.SecretKey,
		Bucket:    bucket,
	}
}

func TestEnsureBucketCreatesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	cfg := minioConfig(t, "pgbackrest")

	exists, err := BucketExists(ctx, cfg)
	if err != nil {
		t.Fatalf("BucketExists: %v", err)
	}
	if exists {
		t.Fatal("bucket should not exist before EnsureBucket")
	}

	if err := EnsureBucket(ctx, cfg); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	if exists, _ = BucketExists(ctx, cfg); !exists {
		t.Fatal("bucket should exist after EnsureBucket")
	}

	// Idempotent: second call is a no-op.
	if err := EnsureBucket(ctx, cfg); err != nil {
		t.Fatalf("second EnsureBucket should be a no-op: %v", err)
	}
}
