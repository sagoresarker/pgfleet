package api

import "net/http"

// securityHeaders adds conservative security headers to every response. The API
// only ever emits JSON, so a maximally-restrictive CSP (deny everything, no
// framing) is safe here and gives any error page or sniffed response zero ambient
// authority — defense-in-depth against the token-bearing dashboard. The dashboard
// documents themselves are served by Next.js, which sets its own CSP.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-XSS-Protection", "0")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}
