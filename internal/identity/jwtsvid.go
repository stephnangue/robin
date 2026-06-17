package identity

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// jwtFetcher is the minimal seam over the SPIFFE Workload API. The production
// adapter (spiffeFetcher) wraps a *workloadapi.JWTSource; tests inject a fake,
// so the cache / refresh / serve-stale logic needs no running SPIRE agent.
type jwtFetcher interface {
	fetch(ctx context.Context, audience string) (token string, expiry time.Time, err error)
	close() error
}

type cachedSVID struct {
	token     string
	expiry    time.Time
	refreshAt time.Time
}

type refreshCall struct {
	done  chan struct{}
	token string
	err   error
}

// JWTSVIDProvider serves a SPIFFE JWT-SVID, refreshing it ahead of expiry and
// serving a still-valid cached token when the Workload API is briefly down.
// FetchJWTSVID is unary (not pushed), so the provider owns the refresh policy.
type JWTSVIDProvider struct {
	fetcher       jwtFetcher
	audience      string
	refreshBefore time.Duration
	now           func() time.Time // injectable clock for tests

	mu       sync.Mutex
	cache    map[string]cachedSVID
	inflight map[string]*refreshCall
}

func newJWTSVIDProviderWith(f jwtFetcher, audience string, refreshBefore time.Duration) *JWTSVIDProvider {
	return &JWTSVIDProvider{
		fetcher:       f,
		audience:      audience,
		refreshBefore: refreshBefore,
		now:           time.Now,
		cache:         map[string]cachedSVID{},
		inflight:      map[string]*refreshCall{},
	}
}

// Token returns a valid JWT-SVID for the configured audience.
func (p *JWTSVIDProvider) Token(ctx context.Context) (string, error) {
	aud := p.audience

	p.mu.Lock()
	c, ok := p.cache[aud]
	p.mu.Unlock()

	now := p.now()
	if ok && now.Before(c.refreshAt) {
		return c.token, nil // fresh — fast path, no fetch
	}

	tok, err := p.refresh(ctx, aud)
	if err != nil {
		if ok && now.Before(c.expiry) {
			return c.token, nil // serve stale: refresh failed but token still valid
		}
		return "", err
	}
	return tok, nil
}

// refresh fetches a new SVID, collapsing concurrent refreshes so a burst of
// requests triggers a single Workload API call.
func (p *JWTSVIDProvider) refresh(ctx context.Context, aud string) (string, error) {
	p.mu.Lock()
	if call, ok := p.inflight[aud]; ok {
		p.mu.Unlock()
		select {
		case <-call.done:
			return call.token, call.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	p.inflight[aud] = call
	p.mu.Unlock()

	tok, exp, err := p.fetcher.fetch(ctx, aud)
	if err != nil {
		tok = ""
		err = fmt.Errorf("identity/jwtsvid: fetch: %w", err)
	}

	p.mu.Lock()
	delete(p.inflight, aud)
	if err == nil {
		p.cache[aud] = cachedSVID{token: tok, expiry: exp, refreshAt: p.refreshAt(exp)}
	}
	p.mu.Unlock()

	// Share one result (and one error shape) with any collapsed waiters.
	call.token, call.err = tok, err
	close(call.done)
	return tok, err
}

// refreshAt returns when a token expiring at exp should be proactively
// refreshed: refreshBefore ahead of exp, clamped to at most half the observed
// lifetime so very short TTLs are not refreshed too eagerly.
func (p *JWTSVIDProvider) refreshAt(exp time.Time) time.Time {
	lead := p.refreshBefore
	if ttl := exp.Sub(p.now()); ttl > 0 && ttl/2 < lead {
		lead = ttl / 2
	}
	return exp.Add(-lead)
}

// Close releases the underlying Workload API source.
func (p *JWTSVIDProvider) Close() error { return p.fetcher.close() }
