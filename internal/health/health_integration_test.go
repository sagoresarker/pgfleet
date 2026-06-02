//go:build integration

package health

import (
	"context"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provisiontest"
)

func TestCheckAndDrillAgainstRealInstance(t *testing.T) {
	inst, prov, repo, rt := provisiontest.ProvisionLocalInstance(t)
	ctx := context.Background()

	// Take a real full backup so there is something to assess and drill.
	res, err := rt.Exec(ctx, inst.ContainerID, asPostgres([]string{
		"pgbackrest", "--config=" + confPath, "--stanza=" + inst.Stanza, "--type=full", "backup",
	}))
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("backup: exit %d err %v\n%s", res.ExitCode, err, res.Stderr)
	}

	// Health check: archiving healthy, recent backup, modest pg_wal.
	checker := NewChecker(rt,
		lookupFunc(func(context.Context, string) (instance.Instance, error) { return inst, nil }),
		listFunc(func(context.Context, string) ([]backup.Backup, error) {
			return []backup.Backup{{Label: "L1", StoppedAt: time.Now()}}, nil
		}),
		DefaultThresholds(),
	)
	report, err := checker.Check(ctx, inst.ID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.ArchivingOK {
		t.Errorf("archiving should be OK after a successful check; issues: %v", report.Issues)
	}
	if !report.Healthy() {
		t.Errorf("instance should be healthy; issues: %v", report.Issues)
	}

	_ = repo // instance repo not needed further

	// Restore drill: the latest backup must actually restore.
	drill, err := prov.RestoreDrill(ctx, inst.ID)
	if err != nil {
		t.Fatalf("RestoreDrill: %v", err)
	}
	if !drill.OK {
		t.Errorf("restore drill failed: %s", drill.Detail)
	}
	if drill.Duration <= 0 {
		t.Error("drill duration should be measured")
	}
}
