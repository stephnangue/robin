package identity

import (
	"context"
	"fmt"

	"github.com/snangue/robin/internal/config"
)

// New builds the identity provider selected by cfg.TokenSource.
func New(ctx context.Context, cfg config.Config) (IdentityProvider, error) {
	switch cfg.TokenSource {
	case "file":
		return NewFileProvider(cfg.TokenFile), nil
	case "jwtsvid":
		// Real implementation lands in PR4 (SPIFFE JWT-SVID provider).
		return nil, fmt.Errorf("identity: token source %q not yet available", cfg.TokenSource)
	default:
		return nil, fmt.Errorf("identity: unknown token source %q", cfg.TokenSource)
	}
}
