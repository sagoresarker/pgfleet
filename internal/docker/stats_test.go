package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

// TestStatsFromResponseBlkio verifies that statsFromResponse sums the Read/Write
// entries of the recursive blkio byte/op counters and reports them as available.
func TestStatsFromResponseBlkio(t *testing.T) {
	s := container.StatsResponse{}
	s.BlkioStats.IoServiceBytesRecursive = []container.BlkioStatEntry{
		{Major: 8, Minor: 0, Op: "Read", Value: 1000},
		{Major: 8, Minor: 0, Op: "Write", Value: 2000},
		{Major: 8, Minor: 16, Op: "Read", Value: 500},
		{Major: 8, Minor: 16, Op: "Write", Value: 250},
		{Major: 8, Minor: 0, Op: "Sync", Value: 9999},  // ignored
		{Major: 8, Minor: 0, Op: "Total", Value: 9999}, // ignored
	}
	s.BlkioStats.IoServicedRecursive = []container.BlkioStatEntry{
		{Major: 8, Minor: 0, Op: "Read", Value: 10},
		{Major: 8, Minor: 0, Op: "Write", Value: 20},
		{Major: 8, Minor: 16, Op: "Read", Value: 3},
		{Major: 8, Minor: 16, Op: "Write", Value: 4},
	}

	out := statsFromResponse(s)

	if !out.DiskIOAvailable {
		t.Fatal("DiskIOAvailable = false, want true when blkio stats are present")
	}
	if out.DiskReadBytes != 1500 {
		t.Errorf("DiskReadBytes = %d, want 1500", out.DiskReadBytes)
	}
	if out.DiskWriteBytes != 2250 {
		t.Errorf("DiskWriteBytes = %d, want 2250", out.DiskWriteBytes)
	}
	if out.DiskReadOps != 13 {
		t.Errorf("DiskReadOps = %d, want 13", out.DiskReadOps)
	}
	if out.DiskWriteOps != 24 {
		t.Errorf("DiskWriteOps = %d, want 24", out.DiskWriteOps)
	}
}

// TestStatsFromResponseBlkioCaseInsensitive verifies the op match tolerates the
// lower-case "read"/"write" spelling some runtimes emit.
func TestStatsFromResponseBlkioCaseInsensitive(t *testing.T) {
	s := container.StatsResponse{}
	s.BlkioStats.IoServiceBytesRecursive = []container.BlkioStatEntry{
		{Op: "read", Value: 100},
		{Op: "write", Value: 200},
	}
	out := statsFromResponse(s)
	if !out.DiskIOAvailable {
		t.Fatal("DiskIOAvailable = false, want true")
	}
	if out.DiskReadBytes != 100 || out.DiskWriteBytes != 200 {
		t.Errorf("read/write bytes = %d/%d, want 100/200", out.DiskReadBytes, out.DiskWriteBytes)
	}
}

// TestStatsFromResponseBlkioEmpty verifies that when the recursive blkio lists
// are empty (e.g. Docker Desktop on macOS / some cgroup v2 setups), DiskIO is
// reported unavailable rather than as a fabricated all-zero measurement.
func TestStatsFromResponseBlkioEmpty(t *testing.T) {
	out := statsFromResponse(container.StatsResponse{})
	if out.DiskIOAvailable {
		t.Error("DiskIOAvailable = true, want false when blkio stats are empty/nil")
	}
	if out.DiskReadBytes != 0 || out.DiskWriteBytes != 0 || out.DiskReadOps != 0 || out.DiskWriteOps != 0 {
		t.Error("DiskIO counters should be zero when blkio is unavailable")
	}
}
