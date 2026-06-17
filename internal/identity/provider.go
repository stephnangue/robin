// Package identity resolves the workload's native identity as a bearer token.
// Each provider yields a token for the configured audience; the proxy applies
// it uniformly without knowing which provider produced it.
package identity

import (
	"context"
	"errors"
)

// IdentityProvider resolves the workload's native identity as a bearer token.
type IdentityProvider interface {
	// Token returns a bearer token valid for the configured audience.
	// Implementations must be safe for concurrent use.
	Token(ctx context.Context) (string, error)
	// Close releases any background resources held by the provider.
	Close() error
}

// Sentinel errors. ErrAudienceMismatch is a fail-closed configuration error,
// not a transient condition.
var (
	ErrNoToken          = errors.New("identity: empty token")
	ErrAudienceMismatch = errors.New("identity: audience mismatch")
)
