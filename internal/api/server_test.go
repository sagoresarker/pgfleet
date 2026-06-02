package api

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeRespondsThenShutsDownGracefully(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, NewRouter(Deps{}), log) }()

	// Server should be answering requests.
	url := "http://" + ln.Addr().String() + "/healthz"
	resp, err := waitForGet(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Cancelling the context should trigger a clean shutdown.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not shut down within 5s")
	}
}

func waitForGet(url string) (*http.Response, error) {
	var lastErr error
	for range 50 {
		resp, err := http.Get(url)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	return nil, lastErr
}
