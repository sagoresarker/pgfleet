package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

type sampleBody struct {
	Name     string `json:"name"`
	Replicas int    `json:"replicas"`
}

func decodeReq(body string) error {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	var dst sampleBody
	return decodeJSON(req, &dst)
}

func TestDecodeJSONValid(t *testing.T) {
	if err := decodeReq(`{"name":"orders","replicas":2}`); err != nil {
		t.Fatalf("valid body should decode: %v", err)
	}
}

func TestDecodeJSONRejections(t *testing.T) {
	cases := map[string]string{
		"empty body":    ``,
		"unknown field": `{"bogus":1}`,
		"trailing data": `{"name":"a"} {"x":1}`,
		"wrong type":    `{"replicas":"five"}`,
		"not json":      `not json`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			err := decodeReq(body)
			if err == nil {
				t.Fatal("expected an error")
			}
			if apperr.HTTPStatus(err) != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", apperr.HTTPStatus(err))
			}
		})
	}
}

func TestDecodeJSONOversizeBody(t *testing.T) {
	big := `{"name":"` + strings.Repeat("a", (1<<20)+10) + `"}`
	err := decodeReq(big)
	if err == nil {
		t.Fatal("oversize body should be rejected")
	}
	if apperr.HTTPStatus(err) != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", apperr.HTTPStatus(err))
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention size limit: %v", err)
	}
}

// TestRespondErrorRedactsInternal — a 5xx must not leak its internal cause.
func TestRespondErrorRedactsInternal(t *testing.T) {
	rr := httptest.NewRecorder()
	respondError(rr, apperr.Wrap(apperr.KindInternal, "issue token", errors.New("pg down: secret-dsn=hunter2")))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "secret-dsn") || strings.Contains(rr.Body.String(), "pg down") {
		t.Errorf("internal cause leaked: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "internal error") {
		t.Errorf("body should be the generic message: %s", rr.Body.String())
	}
}

// TestRespondErrorPassesClientMessage — a 4xx surfaces its (intentional)
// message to the client.
func TestRespondErrorPassesClientMessage(t *testing.T) {
	rr := httptest.NewRecorder()
	respondError(rr, apperr.New(apperr.KindNotFound, "instance not found"))
	if rr.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "instance not found") {
		t.Errorf("client message missing: %s", rr.Body.String())
	}
}
