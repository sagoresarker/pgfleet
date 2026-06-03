package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// maxLogBytes bounds how much of a container's log stream we read, and
// maxLogLines bounds how many trailing lines we return.
const (
	maxLogBytes = 4 << 20 // 4 MiB read cap
	maxLogLines = 1000
)

type logInstanceLookup interface {
	Get(ctx context.Context, id string) (instance.Instance, error)
}

type logRuntime interface {
	Logs(ctx context.Context, id string, follow bool) (io.ReadCloser, error)
}

// LogsHandler serves recent container logs for an instance.
type LogsHandler struct {
	instances logInstanceLookup
	rt        logRuntime
}

// NewLogsHandler builds a LogsHandler.
func NewLogsHandler(instances logInstanceLookup, rt logRuntime) *LogsHandler {
	return &LogsHandler{instances: instances, rt: rt}
}

// Get returns the tail of the instance container's logs as plain text in a JSON
// envelope.
func (h *LogsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inst, err := h.instances.Get(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	if inst.ContainerID == "" {
		respondError(w, apperr.New(apperr.KindConflict, "instance has no running container"))
		return
	}

	rc, err := h.rt.Logs(r.Context(), inst.ContainerID, false)
	if err != nil {
		respondError(w, err)
		return
	}
	defer rc.Close()

	// Docker multiplexes stdout/stderr with an 8-byte frame header per chunk;
	// stdcopy demultiplexes both streams into readable text.
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, io.LimitReader(rc, maxLogBytes)); err != nil && buf.Len() == 0 {
		respondError(w, apperr.Wrap(apperr.KindInternal, "read logs", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"logs": tailLines(buf.String(), maxLogLines)})
}

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
