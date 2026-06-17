package server

import (
	"io"
	"net/http"

	"github.com/snangue/robin/internal/identity"
)

// AdminMux builds the admin handler served on a listener separate from the
// proxy: liveness, readiness, and (in v0.2) metrics. These must not live on the
// proxy listener, which forwards every path to the broker.
func AdminMux(p identity.IdentityProvider) http.Handler {
	mux := http.NewServeMux()

	// Liveness: the process is up.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})

	// Readiness: an identity can actually be resolved.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := p.Token(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ready")
	})

	return mux
}
