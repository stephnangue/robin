package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/snangue/robin/internal/config"
)

// buildTransport clones the default transport (preserving connection-pool and
// timeout defaults) and, when a CA file is configured, pins the broker's trust
// roots for TLS verification.
func buildTransport(cfg config.Config) (*http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("proxy: unexpected default transport type %T", http.DefaultTransport)
	}
	t := base.Clone()
	if cfg.UpstreamCAFile != "" {
		pem, err := os.ReadFile(cfg.UpstreamCAFile)
		if err != nil {
			return nil, fmt.Errorf("proxy: read upstream CA %s: %w", cfg.UpstreamCAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("proxy: no certificates found in %s", cfg.UpstreamCAFile)
		}
		t.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return t, nil
}
