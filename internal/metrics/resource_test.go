package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
)

func TestParseDfBytes(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantTotal int64
		wantUsed  int64
		wantAvail int64
		wantOK    bool
	}{
		{
			name: "typical df -PB1 output",
			in: "Filesystem      1073741824-blocks       Used  Available Capacity Mounted on\n" +
				"/dev/sda1           107374182400 21474836480 85899345920      20% /var/lib/postgresql/data\n",
			wantTotal: 107374182400,
			wantUsed:  21474836480,
			wantAvail: 85899345920,
			wantOK:    true,
		},
		{
			name: "extra leading whitespace and trailing newline",
			in: "Filesystem 1B-blocks Used Available Use% Mounted on\n" +
				"   overlay   1000   400   600   40%   /var/lib/postgresql/data\n\n",
			wantTotal: 1000,
			wantUsed:  400,
			wantAvail: 600,
			wantOK:    true,
		},
		{
			name:   "empty input",
			in:     "",
			wantOK: false,
		},
		{
			name:   "header only",
			in:     "Filesystem 1B-blocks Used Available Use% Mounted on\n",
			wantOK: false,
		},
		{
			name:   "malformed data line, too few fields",
			in:     "Filesystem 1B-blocks Used Available Use% Mounted on\noverlay 1000 400\n",
			wantOK: false,
		},
		{
			name:   "non-numeric fields",
			in:     "Filesystem 1B-blocks Used Available Use% Mounted on\noverlay abc def ghi 40% /data\n",
			wantOK: false,
		},
		{
			name: "mount path with spaces still parses (path is last)",
			in: "Filesystem 1B-blocks Used Available Use% Mounted on\n" +
				"overlay 2000 500 1500 25% /var/lib/postgresql/data\n",
			wantTotal: 2000,
			wantUsed:  500,
			wantAvail: 1500,
			wantOK:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, used, avail, ok := parseDfBytes(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if total != tt.wantTotal || used != tt.wantUsed || avail != tt.wantAvail {
				t.Errorf("got (total=%d used=%d avail=%d), want (total=%d used=%d avail=%d)",
					total, used, avail, tt.wantTotal, tt.wantUsed, tt.wantAvail)
			}
		})
	}
}

func samplesByMetric(s []Sample) map[string]Sample {
	m := map[string]Sample{}
	for _, sm := range s {
		m[sm.Metric] = sm
	}
	return m
}

func TestResourceCollectorCollect(t *testing.T) {
	rt := docker.NewFake()
	id, err := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "pg"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.StartContainer(context.Background(), id); err != nil {
		t.Fatal(err)
	}

	rt.StatsFunc = func(string) (docker.ContainerStats, error) {
		return docker.ContainerStats{
			CPUPercent:       42.5,
			MemoryBytes:      512 << 20,
			MemoryLimitBytes: 1 << 30,
			MemoryPercent:    50.0,
		}, nil
	}
	rt.ExecFunc = func(_ string, _ []string) (docker.ExecResult, error) {
		// total 1000, used 250, avail 750 -> free% = 75
		return docker.ExecResult{
			ExitCode: 0,
			Stdout: "Filesystem 1B-blocks Used Available Use% Mounted on\n" +
				"overlay 1000 250 750 25% /var/lib/postgresql/data\n",
		}, nil
	}

	fixed := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	c := NewResourceCollector(rt)
	c.now = func() time.Time { return fixed }

	got, err := c.Collect(context.Background(), "inst-1", id)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	by := samplesByMetric(got)
	if _, ok := by["cpu_percent"]; ok {
		t.Error("cpu_percent must not be emitted from a one-shot stats sample (REG-6)")
	}
	want := map[string]float64{
		"memory_bytes":      float64(512 << 20),
		"memory_percent":    50.0,
		"disk_total_bytes":  1000,
		"disk_used_bytes":   250,
		"disk_free_percent": 75,
	}
	for metric, val := range want {
		sm, ok := by[metric]
		if !ok {
			t.Errorf("missing metric %q", metric)
			continue
		}
		if sm.Value != val {
			t.Errorf("metric %q = %v, want %v", metric, sm.Value, val)
		}
		if sm.InstanceID != "inst-1" {
			t.Errorf("metric %q InstanceID = %q, want inst-1", metric, sm.InstanceID)
		}
		if !sm.At.Equal(fixed) {
			t.Errorf("metric %q At = %v, want %v", metric, sm.At, fixed)
		}
	}
}

// TestResourceCollectorOmitsCPUPercent verifies REG-6: the one-shot Docker
// stats sample carries an empty PreCPUStats, so the CPUPercent derived from it
// is not a meaningful instantaneous gauge. The collector must not emit a
// fabricated cpu_percent. It exposes the meaningful memory and disk metrics
// instead.
func TestResourceCollectorOmitsCPUPercent(t *testing.T) {
	rt := docker.NewFake()
	id, err := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "pg"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.StartContainer(context.Background(), id); err != nil {
		t.Fatal(err)
	}

	rt.StatsFunc = func(string) (docker.ContainerStats, error) {
		// CPUPercent here is whatever the one-shot path produced; it is
		// meaningless and must not surface as a sample.
		return docker.ContainerStats{
			CPUPercent:    9999.0,
			MemoryBytes:   256 << 20,
			MemoryPercent: 25.0,
		}, nil
	}
	rt.ExecFunc = func(_ string, _ []string) (docker.ExecResult, error) {
		return docker.ExecResult{
			ExitCode: 0,
			Stdout: "Filesystem 1B-blocks Used Available Use% Mounted on\n" +
				"overlay 1000 250 750 25% /var/lib/postgresql/data\n",
		}, nil
	}

	got, err := NewResourceCollector(rt).Collect(context.Background(), "inst-1", id)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	by := samplesByMetric(got)
	if _, ok := by["cpu_percent"]; ok {
		t.Error("cpu_percent must not be emitted: a one-shot sample has empty PreCPUStats and the value is meaningless")
	}
	// Meaningful metrics are still present.
	for _, m := range []string{"memory_bytes", "memory_percent", "disk_total_bytes", "disk_used_bytes", "disk_free_percent"} {
		if _, ok := by[m]; !ok {
			t.Errorf("expected meaningful metric %q to be emitted", m)
		}
	}
}

func TestResourceCollectorDiskOnlyWhenStatsFail(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "pg"})
	_ = rt.StartContainer(context.Background(), id)

	rt.StatsFunc = func(string) (docker.ContainerStats, error) {
		return docker.ContainerStats{}, errors.New("stats unavailable")
	}
	rt.ExecFunc = func(_ string, _ []string) (docker.ExecResult, error) {
		return docker.ExecResult{
			ExitCode: 0,
			Stdout: "Filesystem 1B-blocks Used Available Use% Mounted on\n" +
				"overlay 1000 100 900 10% /var/lib/postgresql/data\n",
		}, nil
	}

	got, err := NewResourceCollector(rt).Collect(context.Background(), "inst-1", id)
	if err != nil {
		t.Fatalf("Collect should succeed when only stats fail: %v", err)
	}
	by := samplesByMetric(got)
	if _, ok := by["cpu_percent"]; ok {
		t.Error("did not expect cpu_percent when stats failed")
	}
	if sm, ok := by["disk_free_percent"]; !ok || sm.Value != 90 {
		t.Errorf("disk_free_percent = %v (ok=%v), want 90", sm.Value, ok)
	}
}

func TestResourceCollectorStatsOnlyWhenDiskFails(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "pg"})
	_ = rt.StartContainer(context.Background(), id)

	rt.StatsFunc = func(string) (docker.ContainerStats, error) {
		return docker.ContainerStats{CPUPercent: 10, MemoryBytes: 1, MemoryPercent: 2}, nil
	}
	rt.ExecFunc = func(_ string, _ []string) (docker.ExecResult, error) {
		return docker.ExecResult{ExitCode: 1, Stderr: "df: no such directory"}, nil
	}

	got, err := NewResourceCollector(rt).Collect(context.Background(), "inst-1", id)
	if err != nil {
		t.Fatalf("Collect should succeed when only disk fails: %v", err)
	}
	by := samplesByMetric(got)
	if _, ok := by["memory_bytes"]; !ok {
		t.Error("expected memory_bytes when stats succeeded")
	}
	if _, ok := by["cpu_percent"]; ok {
		t.Error("cpu_percent must not be emitted (REG-6)")
	}
	if _, ok := by["disk_free_percent"]; ok {
		t.Error("did not expect disk metrics when df failed")
	}
}

func TestResourceCollectorErrorsWhenBothFail(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "pg"})
	_ = rt.StartContainer(context.Background(), id)

	rt.StatsFunc = func(string) (docker.ContainerStats, error) {
		return docker.ContainerStats{}, errors.New("stats down")
	}
	rt.ExecFunc = func(_ string, _ []string) (docker.ExecResult, error) {
		return docker.ExecResult{}, errors.New("exec down")
	}

	_, err := NewResourceCollector(rt).Collect(context.Background(), "inst-1", id)
	if err == nil {
		t.Fatal("expected error when both stats and disk fail")
	}
	if apperr.Kind(err) != apperr.KindInternal {
		t.Errorf("error kind = %v, want KindInternal", apperr.Kind(err))
	}
}
