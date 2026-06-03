package health

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/docker"
)

// TestDuExecErrorIsFlagged — when the pg_wal du probe ERRORS, the instance
// must NOT be silently reported healthy. A failed probe is itself an issue.
func TestDuExecErrorIsFlagged(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if cmd[len(cmd)-1] == "check" {
			return docker.ExecResult{ExitCode: 0}, nil
		}
		if cmd[0] == "du" {
			return docker.ExecResult{}, errors.New("exec failed")
		}
		return docker.ExecResult{}, nil
	}
	c := newChecker(rt, []backup.Backup{recentBackup()}, DefaultThresholds())

	r, err := c.Check(context.Background(), "i1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Healthy() {
		t.Error("a failed pg_wal probe must make the instance unhealthy")
	}
}

// TestDuNonZeroExitIsFlagged — a non-zero du exit (probe broken) must also be
// flagged rather than hidden.
func TestDuNonZeroExitIsFlagged(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if cmd[len(cmd)-1] == "check" {
			return docker.ExecResult{ExitCode: 0}, nil
		}
		if cmd[0] == "du" {
			return docker.ExecResult{ExitCode: 1, Stderr: "du: cannot access"}, nil
		}
		return docker.ExecResult{}, nil
	}
	c := newChecker(rt, []backup.Backup{recentBackup()}, DefaultThresholds())

	r, _ := c.Check(context.Background(), "i1")
	if r.Healthy() {
		t.Error("a non-zero du exit must make the instance unhealthy")
	}
}

func TestParseDuBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"   ", 0},
		{"12345\t/path", 12345},
		{"abc\t/path", 0},
		{"99999999999999999999\t/p", math.MaxInt64}, // overflow must saturate, not become 0
		{"-5\t/path", 0},                             // negative is nonsensical for du; clamp to 0
	}
	for _, tc := range cases {
		if got := parseDuBytes(tc.in); got != tc.want {
			t.Errorf("parseDuBytes(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestNoCompletedBackupNotReportedAsAncient — if every catalog row has a zero
// StoppedAt (no backup ever completed), the age must not be computed against
// the zero time (which would falsely read as "decades old").
func TestNoCompletedBackupNotReportedAsAncient(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = execScript(0, "1024")
	// A backup row that never finished (zero StoppedAt).
	bk := backup.Backup{Label: "L1", Type: "full"}
	c := newChecker(rt, []backup.Backup{bk}, DefaultThresholds())

	r, _ := c.Check(context.Background(), "i1")
	for _, iss := range r.Issues {
		if len(iss) >= 11 && iss[:11] == "last backup" {
			t.Errorf("zero StoppedAt should not produce a stale-age issue, got %q", iss)
		}
	}
}
