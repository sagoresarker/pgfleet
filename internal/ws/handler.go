package ws

import (
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	// The control plane is same-origin in production; the frontend connects
	// from the same host. Tighten if cross-origin is ever needed.
	CheckOrigin: func(*http.Request) bool { return true },
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
				if err := conn.WriteJSON(ev); err != nil {
					return
				}
			case <-closed:
				return
			}
		}
	}
}
