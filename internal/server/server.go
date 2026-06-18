// Package server runs Robin's two listeners — the proxy plane and a separate
// admin plane (health/readiness/metrics) — with graceful, drain-on-SIGTERM
// shutdown so in-flight egress is not severed during pod termination.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/snangue/robin/internal/config"
)

// shutdownTimeout bounds the in-flight drain. Deployments must set their
// termination grace period strictly larger (see deploy/ in v0.2).
const shutdownTimeout = 25 * time.Second

// Server owns the proxy and admin listeners.
type Server struct {
	proxy   *http.Server
	admin   *http.Server
	proxyLn net.Listener
	adminLn net.Listener
	log     *slog.Logger
}

// New binds both listeners (proxy: TCP or UDS; admin: TCP) up front, so their
// resolved addresses are available before Run and addressable in tests.
func New(cfg config.Config, proxyHandler, adminHandler http.Handler, log *slog.Logger) (*Server, error) {
	proxyLn, err := proxyListener(cfg)
	if err != nil {
		return nil, err
	}
	adminLn, err := net.Listen("tcp", cfg.AdminAddr)
	if err != nil {
		_ = proxyLn.Close()
		return nil, fmt.Errorf("server: listen admin %s: %w", cfg.AdminAddr, err)
	}
	return &Server{
		proxy:   &http.Server{Handler: proxyHandler},
		admin:   &http.Server{Handler: adminHandler},
		proxyLn: proxyLn,
		adminLn: adminLn,
		log:     log,
	}, nil
}

// ProxyAddr returns the resolved proxy listener address.
func (s *Server) ProxyAddr() net.Addr { return s.proxyLn.Addr() }

// AdminAddr returns the resolved admin listener address.
func (s *Server) AdminAddr() net.Addr { return s.adminLn.Addr() }

// Run serves both planes until ctx is canceled (SIGTERM), then drains
// in-flight requests within shutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	errc := make(chan error, 2)
	serve := func(srv *http.Server, ln net.Listener) {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}
	go serve(s.proxy, s.proxyLn)
	go serve(s.admin, s.adminLn)

	s.log.Info("robin started",
		slog.String("proxy", s.proxyLn.Addr().String()),
		slog.String("admin", s.adminLn.Addr().String()))

	shutdown := func() error {
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		// Drain the proxy first (finish in-flight egress), then the admin plane.
		return errors.Join(s.proxy.Shutdown(shutCtx), s.admin.Shutdown(shutCtx))
	}

	select {
	case err := <-errc:
		// One plane failed to serve: shut the other down too, never leave it running.
		return errors.Join(err, shutdown())
	case <-ctx.Done():
		s.log.Info("shutting down, draining in-flight egress")
		return shutdown()
	}
}
