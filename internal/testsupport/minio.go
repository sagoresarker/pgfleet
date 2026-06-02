//go:build integration

package testsupport

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/minio"
)

// MinIO describes a running MinIO test container.
type MinIO struct {
	Endpoint  string // host:port reachable from the test host
	AccessKey string
	SecretKey string
}

// StartMinIO launches a throwaway MinIO and returns its connection details.
func StartMinIO(t *testing.T) MinIO {
	t.Helper()
	ctx := context.Background()

	mc, err := minio.Run(ctx, "minio/minio:latest",
		minio.WithUsername("pgfleet"),
		minio.WithPassword("pgfleetpgfleet"),
	)
	if err != nil {
		t.Fatalf("start minio: %v", err)
	}
	t.Cleanup(func() { _ = mc.Terminate(ctx) })

	endpoint, err := mc.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("minio connection string: %v", err)
	}
	return MinIO{Endpoint: endpoint, AccessKey: mc.Username, SecretKey: mc.Password}
}
