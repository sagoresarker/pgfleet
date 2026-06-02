package api

import (
	"encoding/json"
	"net/http"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// maxBodyBytes bounds request bodies to defend against memory-exhaustion.
const maxBodyBytes = 1 << 20 // 1 MiB

// decodeJSON strictly decodes a JSON request body into dst, rejecting unknown
// fields, trailing data, and oversized bodies.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
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
