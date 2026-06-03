package api

import (
	"context"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/timescale"
)

// tsInstanceLookup is the subset of the instance repository the handler needs.
type tsInstanceLookup interface {
	Get(ctx context.Context, id string) (instance.Instance, error)
}

// DSNResolver returns a connection string for an instance (plaintext password).
type DSNResolver func(ctx context.Context, instanceID string) (string, error)

// tsSvc is the stateless TimescaleDB management service; the connection is
// supplied per call.
var tsSvc = timescale.NewService()

// TimescaleHandler manages TimescaleDB objects on a managed instance. Each
// endpoint opens a fresh connection to the target instance, performs one
// management action, and closes it.
type TimescaleHandler struct {
	instances tsInstanceLookup
	dsn       DSNResolver
}

// NewTimescaleHandler builds a TimescaleHandler.
func NewTimescaleHandler(instances tsInstanceLookup, dsn DSNResolver) *TimescaleHandler {
	return &TimescaleHandler{instances: instances, dsn: dsn}
}

// connect looks up the instance, verifies TimescaleDB is enabled, and opens a
// connection to it. On any failure it writes an error response and returns
// ok=false; otherwise the caller owns conn and must defer conn.Close(ctx).
func (h *TimescaleHandler) connect(w http.ResponseWriter, r *http.Request) (*pgx.Conn, instance.Instance, bool) {
	ctx := r.Context()
	inst, err := h.instances.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return nil, instance.Instance{}, false
	}
	if !slices.Contains(inst.Extensions, "timescaledb") {
		respondError(w, apperr.New(apperr.KindConflict, "timescaledb is not enabled on this instance"))
		return nil, instance.Instance{}, false
	}
	dsn, err := h.dsn(ctx, inst.ID)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "timescale: resolve dsn", err))
		return nil, instance.Instance{}, false
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "timescale: connect", err))
		return nil, instance.Instance{}, false
	}
	return conn, inst, true
}

// List returns every hypertable on the instance.
func (h *TimescaleHandler) List(w http.ResponseWriter, r *http.Request) {
	conn, _, ok := h.connect(w, r)
	if !ok {
		return
	}
	defer conn.Close(r.Context())

	tables, err := tsSvc.ListHypertables(r.Context(), conn)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hypertables": tables})
}

type createHypertableRequest struct {
	Table      string `json:"table"`
	TimeColumn string `json:"time_column"`
}

// CreateHypertable converts a table into a hypertable.
func (h *TimescaleHandler) CreateHypertable(w http.ResponseWriter, r *http.Request) {
	var req createHypertableRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	conn, _, ok := h.connect(w, r)
	if !ok {
		return
	}
	defer conn.Close(r.Context())

	if err := tsSvc.CreateHypertable(r.Context(), conn, req.Table, req.TimeColumn); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "ok"})
}

type retentionRequest struct {
	Hypertable string `json:"hypertable"`
	DropAfter  string `json:"drop_after"`
}

// AddRetention adds a data-retention policy to a hypertable.
func (h *TimescaleHandler) AddRetention(w http.ResponseWriter, r *http.Request) {
	var req retentionRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	conn, _, ok := h.connect(w, r)
	if !ok {
		return
	}
	defer conn.Close(r.Context())

	if err := tsSvc.AddRetentionPolicy(r.Context(), conn, req.Hypertable, req.DropAfter); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type hypertableRequest struct {
	Hypertable string `json:"hypertable"`
}

// RemoveRetention removes the retention policy from a hypertable. The hypertable
// may be given in the request body or as a ?hypertable= query parameter.
func (h *TimescaleHandler) RemoveRetention(w http.ResponseWriter, r *http.Request) {
	hypertable, err := h.hypertableParam(r)
	if err != nil {
		respondError(w, err)
		return
	}
	conn, _, ok := h.connect(w, r)
	if !ok {
		return
	}
	defer conn.Close(r.Context())

	if err := tsSvc.RemoveRetentionPolicy(r.Context(), conn, hypertable); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type enableCompressionRequest struct {
	Hypertable    string `json:"hypertable"`
	SegmentBy     string `json:"segment_by"`
	OrderBy       string `json:"order_by"`
	CompressAfter string `json:"compress_after"`
}

// EnableCompression enables columnar compression on a hypertable and, when
// compress_after is given, schedules an automatic compression policy.
func (h *TimescaleHandler) EnableCompression(w http.ResponseWriter, r *http.Request) {
	var req enableCompressionRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	conn, _, ok := h.connect(w, r)
	if !ok {
		return
	}
	defer conn.Close(r.Context())

	if err := tsSvc.EnableCompression(r.Context(), conn, req.Hypertable, req.SegmentBy, req.OrderBy); err != nil {
		respondError(w, err)
		return
	}
	if req.CompressAfter != "" {
		if err := tsSvc.AddCompressionPolicy(r.Context(), conn, req.Hypertable, req.CompressAfter); err != nil {
			respondError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// RemoveCompression removes the compression policy from a hypertable. The
// hypertable may be given in the request body or as a ?hypertable= query
// parameter.
func (h *TimescaleHandler) RemoveCompression(w http.ResponseWriter, r *http.Request) {
	hypertable, err := h.hypertableParam(r)
	if err != nil {
		respondError(w, err)
		return
	}
	conn, _, ok := h.connect(w, r)
	if !ok {
		return
	}
	defer conn.Close(r.Context())

	if err := tsSvc.RemoveCompressionPolicy(r.Context(), conn, hypertable); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type continuousAggregateRequest struct {
	Name             string `json:"name"`
	Query            string `json:"query"`
	StartOffset      string `json:"start_offset"`
	EndOffset        string `json:"end_offset"`
	ScheduleInterval string `json:"schedule_interval"`
}

// CreateContinuousAggregate creates a continuous aggregate and, when refresh
// offsets are supplied, attaches a refresh policy.
func (h *TimescaleHandler) CreateContinuousAggregate(w http.ResponseWriter, r *http.Request) {
	var req continuousAggregateRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	conn, _, ok := h.connect(w, r)
	if !ok {
		return
	}
	defer conn.Close(r.Context())

	if err := tsSvc.CreateContinuousAggregate(r.Context(), conn, req.Name, req.Query); err != nil {
		respondError(w, err)
		return
	}
	if req.StartOffset != "" || req.EndOffset != "" || req.ScheduleInterval != "" {
		if err := tsSvc.AddContinuousAggregatePolicy(r.Context(), conn, req.Name, req.StartOffset, req.EndOffset, req.ScheduleInterval); err != nil {
			respondError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "ok"})
}

// Jobs returns the TimescaleDB background jobs (policy schedules) on the instance.
func (h *TimescaleHandler) Jobs(w http.ResponseWriter, r *http.Request) {
	conn, _, ok := h.connect(w, r)
	if !ok {
		return
	}
	defer conn.Close(r.Context())

	jobs, err := tsSvc.ListJobs(r.Context(), conn)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// hypertableParam reads the target hypertable from the JSON body when present,
// falling back to the ?hypertable= query parameter, so delete endpoints work
// with or without a body.
func (h *TimescaleHandler) hypertableParam(r *http.Request) (string, error) {
	if q := r.URL.Query().Get("hypertable"); q != "" {
		return q, nil
	}
	var req hypertableRequest
	if err := decodeJSON(r, &req); err != nil {
		return "", err
	}
	if req.Hypertable == "" {
		return "", apperr.New(apperr.KindInvalid, "hypertable is required")
	}
	return req.Hypertable, nil
}
