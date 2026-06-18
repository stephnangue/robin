package server

import (
	"fmt"
	"net"
	"os"

	"github.com/snangue/robin/internal/config"
)

// proxyListener builds the proxy listener: a Unix domain socket when configured
// (peer-cred hardening lands in v0.2), otherwise a TCP listener.
func proxyListener(cfg config.Config) (net.Listener, error) {
	if cfg.ListenUDS != "" {
		// Remove a stale socket left behind by a previous run.
		if err := os.Remove(cfg.ListenUDS); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("server: remove stale socket %s: %w", cfg.ListenUDS, err)
		}
		ln, err := net.Listen("unix", cfg.ListenUDS)
		if err != nil {
			return nil, fmt.Errorf("server: listen unix %s: %w", cfg.ListenUDS, err)
		}
		// 0o600 until v0.2 SO_PEERCRED enforcement lands: owner-only, not group-wide.
		if err := os.Chmod(cfg.ListenUDS, 0o600); err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("server: chmod %s: %w", cfg.ListenUDS, err)
		}
		return ln, nil
	}
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("server: listen tcp %s: %w", cfg.ListenAddr, err)
	}
	return ln, nil
}
