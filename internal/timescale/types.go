package timescale

import "time"

// Hypertable summarizes a TimescaleDB hypertable and its physical footprint.
type Hypertable struct {
	Schema             string `json:"schema"`
	Name               string `json:"name"`
	NumChunks          int64  `json:"num_chunks"`
	SizeBytes          int64  `json:"size_bytes"`
	CompressionEnabled bool   `json:"compression_enabled"`
}

// Job is a TimescaleDB background job (a policy's scheduled action) as reported
// by timescaledb_information.jobs.
type Job struct {
	ID               int64      `json:"id"`
	Application      string     `json:"application"`
	ScheduleInterval string     `json:"schedule_interval"`
	NextStart        *time.Time `json:"next_start,omitempty"`
	LastRunStatus    string     `json:"last_run_status"`
}
