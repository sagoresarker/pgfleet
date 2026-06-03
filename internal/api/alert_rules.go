package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/alerts"
)

// RuleStore is the subset of the alert-rule repository the handler needs.
type RuleStore interface {
	Create(ctx context.Context, r alerts.Rule) (alerts.Rule, error)
	List(ctx context.Context) ([]alerts.Rule, error)
	Update(ctx context.Context, r alerts.Rule) error
	Delete(ctx context.Context, id string) error
}

// AlertRulesHandler serves user-configurable alert-rule CRUD.
type AlertRulesHandler struct {
	store RuleStore
	audit AuditRecorder
}

// NewAlertRulesHandler builds an AlertRulesHandler.
func NewAlertRulesHandler(store RuleStore) *AlertRulesHandler {
	return &AlertRulesHandler{store: store}
}

// WithAudit attaches an audit recorder.
func (h *AlertRulesHandler) WithAudit(rec AuditRecorder) *AlertRulesHandler {
	h.audit = rec
	return h
}

type alertRuleRequest struct {
	InstanceID *string `json:"instance_id"`
	Kind       string  `json:"kind"`
	Threshold  float64 `json:"threshold"`
	Severity   string  `json:"severity"`
	Enabled    bool    `json:"enabled"`
}

func (req alertRuleRequest) toRule(id string) alerts.Rule {
	return alerts.Rule{
		ID:         id,
		InstanceID: req.InstanceID,
		Kind:       req.Kind,
		Threshold:  req.Threshold,
		Severity:   req.Severity,
		Enabled:    req.Enabled,
	}
}

// Create validates and persists a new alert rule (201).
func (h *AlertRulesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req alertRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	rule := req.toRule("")
	if err := rule.Validate(); err != nil {
		respondError(w, err)
		return
	}
	created, err := h.store.Create(r.Context(), rule)
	if err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, "alertrule.create", created.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"rule": created})
}

// List returns all alert rules.
func (h *AlertRulesHandler) List(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	if rules == nil {
		rules = []alerts.Rule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// Update validates and overwrites an existing alert rule by {id} (200).
func (h *AlertRulesHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req alertRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	rule := req.toRule(id)
	if err := rule.Validate(); err != nil {
		respondError(w, err)
		return
	}
	if err := h.store.Update(r.Context(), rule); err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, "alertrule.update", id)
	writeJSON(w, http.StatusOK, map[string]any{"rule": rule})
}

// Delete removes an alert rule by {id} (204).
func (h *AlertRulesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id); err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, "alertrule.delete", id)
	w.WriteHeader(http.StatusNoContent)
}
