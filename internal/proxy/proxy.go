// Package proxy is Robin's reverse-proxy core: it resolves the workload's
// identity and injects it as a bearer token on every request forwarded to the
// broker. It is body-agnostic and streaming — the request body is never read.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/snangue/robin/internal/config"
	"github.com/snangue/robin/internal/identity"
	"github.com/snangue/robin/internal/obs"
)

type ctxKey int

const tokenCtxKey ctxKey = 0

// Handler resolves identity and proxies requests to the broker, injecting the
// token as a bearer header.
type Handler struct {
	provider identity.IdentityProvider
	rp       *httputil.ReverseProxy
	log      *slog.Logger
	source   string
	audience string
}

// New builds a proxy Handler targeting cfg.UpstreamURL.
func New(cfg config.Config, p identity.IdentityProvider, log *slog.Logger) (*Handler, error) {
	target, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: parse upstream URL: %w", err)
	}
	transport, err := buildTransport(cfg)
	if err != nil {
		return nil, err
	}

	h := &Handler{
		provider: p,
		log:      log,
		source:   cfg.TokenSource,
		audience: cfg.Audience,
	}
	h.rp = &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Inject identity, overwriting any inbound placeholder credential.
			tok, _ := pr.In.Context().Value(tokenCtxKey).(string)
			pr.Out.Header.Set("Authorization", "Bearer "+tok)
		},
		ModifyResponse: func(resp *http.Response) error {
			// One access-log line per request, with the broker's status.
			h.log.Info("request injected",
				slog.String(obs.FieldProvider, h.source),
				slog.String(obs.FieldAudience, h.audience),
				slog.String(obs.FieldDecision, obs.DecisionInjected),
				slog.Int(obs.FieldUpstreamStatus, resp.StatusCode))
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			// Single attempt — surface as 502, never retry into a thundering herd.
			h.log.Error("broker unreachable",
				slog.String(obs.FieldProvider, h.source),
				slog.String(obs.FieldDecision, obs.DecisionUpstreamError),
				slog.String(obs.FieldError, err.Error()))
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return h, nil
}

// ServeHTTP resolves the workload's identity and forwards the request. If the
// identity cannot be resolved it returns 503 and never contacts the broker.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok, err := h.provider.Token(r.Context())
	if err == nil && tok == "" {
		// Fail closed: never forward an empty/placeholder credential.
		err = errors.New("provider returned an empty token")
	}
	if err != nil {
		h.log.Warn("identity unavailable",
			slog.String(obs.FieldProvider, h.source),
			slog.String(obs.FieldDecision, obs.DecisionFailed),
			slog.String(obs.FieldError, err.Error()))
		http.Error(w, "identity unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx := context.WithValue(r.Context(), tokenCtxKey, tok)
	h.rp.ServeHTTP(w, r.WithContext(ctx))
}
