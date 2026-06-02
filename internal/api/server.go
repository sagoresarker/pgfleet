package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

// Serve runs an HTTP server on the given listener until ctx is cancelled, then
// shuts it down gracefully (draining in-flight requests up to shutdownTimeout).
// It returns nil on a clean shutdown.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	log.Info("http server listening", "addr", ln.Addr().String())

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		log.Info("http server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
