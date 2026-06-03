package ws

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	// Same-origin only: a cross-origin page must NOT be able to open an
	// authenticated events stream (the token travels in the query string, so an
	// open origin would remove the same-origin guard). An absent Origin header
	// (non-browser client) is allowed; a present one must match the request Host.
	CheckOrigin: sameOrigin,
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// VerifyFunc validates a token string (e.g. a JWT passed as a query param,
// since browsers cannot set headers on WebSocket connections).
type VerifyFunc func(token string) error

// Handler upgrades the request to a WebSocket and streams hub events as JSON.
// It authenticates via the "token" query parameter before upgrading.
func Handler(hub *Hub, verify VerifyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if verify != nil {
			if err := verify(r.URL.Query().Get("token")); err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return // Upgrade already wrote an error response
		}
		defer conn.Close()

		sub, cancel := hub.Subscribe()
		defer cancel()

		// Reader goroutine to detect client disconnect/close frames.
		closed := make(chan struct{})
		go func() {
			defer close(closed)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		for {
			select {
			case ev := <-sub:
				// Bound writes so a half-open/slow client cannot hang the
				// writer goroutine indefinitely.
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteJSON(ev); err != nil {
					return
				}
			case <-closed:
				return
			}
		}
	}
}
