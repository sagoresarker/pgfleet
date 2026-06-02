package api

import (
	"encoding/json"
	"net/http"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// decodeJSON strictly decodes a JSON request body into dst, rejecting unknown
// fields and trailing data.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperr.Wrap(apperr.KindInvalid, "invalid request body", err)
	}
	return nil
}

// respondError maps an error to its HTTP status (via apperr) and writes a JSON
// error body. Internal errors do not leak their message to the client.
func respondError(w http.ResponseWriter, err error) {
	status := apperr.HTTPStatus(err)
	msg := err.Error()
	if status >= http.StatusInternalServerError {
		msg = "internal error"
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
