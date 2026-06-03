package metrics

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
)

// pgDataPath is the data directory mounted from the instance's data volume.
// A full data volume is the top silent killer, so its free space is the
// highest-priority resource metric this collector emits.
const pgDataPath = "/var/lib/postgresql/data"

// ResourceCollector gathers container-level resource metrics (CPU, memory, and
// the data-volume disk usage) for a single managed instance container.
type ResourceCollector struct {
	rt  docker.ContainerRuntime
	now func() time.Time
}

// NewResourceCollector builds a ResourceCollector over the given runtime.
func NewResourceCollector(rt docker.ContainerRuntime) *ResourceCollector {
	return &ResourceCollector{rt: rt, now: time.Now}
}

// Collect gathers resource samples for one instance container. It is
// best-effort: CPU/memory and disk are probed independently and a failure of
// one does not suppress the other. An error is returned only if both probes
// fail (i.e. no samples could be gathered).
func (c *ResourceCollector) Collect(ctx context.Context, instanceID, containerID string) ([]Sample, error) {
	at := c.now()
	var s []Sample
	add := func(metric string, value float64) {
		s = append(s, Sample{InstanceID: instanceID, Metric: metric, Value: value, At: at})
	}

	statsErr := c.collectStats(ctx, containerID, add)
	diskErr := c.collectDisk(ctx, containerID, add)

	if statsErr != nil && diskErr != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "metrics: collect resource stats", statsErr)
	}
	return s, nil
}

// collectStats adds memory samples from the container runtime stats.
//
// REG-6: the runtime gathers stats via a one-shot Docker sample, which carries
// an empty PreCPUStats. A CPU percentage derived from that single sample has no
// prior baseline to delta against, so it is not a meaningful instantaneous
// gauge (it collapses to lifetime-CPU / lifetime-system-time, and frequently to
// a garbage value or zero). Rather than emit a fabricated cpu_percent, the
// collector deliberately omits it and exposes only the memory metrics that are
// meaningful from a single snapshot. Disk usage is gathered separately in
// collectDisk.
func (c *ResourceCollector) collectStats(ctx context.Context, containerID string, add func(string, float64)) error {
	st, err := c.rt.ContainerStats(ctx, containerID)
	if err != nil {
		return err
	}
	add("memory_bytes", float64(st.MemoryBytes))
	add("memory_percent", st.MemoryPercent)
	return nil
}

// collectDisk adds data-volume disk samples by running `df` inside the
// container and parsing its output.
func (c *ResourceCollector) collectDisk(ctx context.Context, containerID string, add func(string, float64)) error {
	res, err := c.rt.Exec(ctx, containerID, []string{"df", "-PB1", pgDataPath})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return apperr.New(apperr.KindInternal, "metrics: df exited "+strconv.Itoa(res.ExitCode))
	}
	total, used, avail, ok := parseDfBytes(res.Stdout)
	if !ok {
		return apperr.New(apperr.KindInternal, "metrics: unparseable df output")
	}
	add("disk_total_bytes", float64(total))
	add("disk_used_bytes", float64(used))
	if total > 0 {
		add("disk_free_percent", float64(avail)/float64(total)*100)
	}
	return nil
}

// parseDfBytes parses the byte counts from `df -PB1`-style output. It expects a
// header line followed by a data line whose fields are: Filesystem, size, Used,
// Available, Capacity%, Mounted-on. ok is false for empty, header-only, or
// otherwise malformed input.
func parseDfBytes(out string) (total, used, avail int64, ok bool) {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		return 0, 0, 0, false
	}
	// The data line is the second non-empty line.
	var dataLine string
	seen := 0
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		seen++
		if seen == 2 {
			dataLine = ln
			break
		}
	}
	if dataLine == "" {
		return 0, 0, 0, false
	}
	fields := strings.Fields(dataLine)
	// Filesystem, size, used, available, capacity, mount — at least 6 fields.
	if len(fields) < 6 {
		return 0, 0, 0, false
	}
	t, err1 := strconv.ParseInt(fields[1], 10, 64)
	u, err2 := strconv.ParseInt(fields[2], 10, 64)
	a, err3 := strconv.ParseInt(fields[3], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	if t < 0 || u < 0 || a < 0 {
		return 0, 0, 0, false
	}
	return t, u, a, true
}
