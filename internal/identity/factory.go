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
		fetcher, err := newJWTSVIDSource(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return newJWTSVIDProviderWith(fetcher, cfg.Audience, cfg.SVIDRefreshBefore), nil
	default:
		return nil, fmt.Errorf("identity: unknown token source %q", cfg.TokenSource)
	}
}
