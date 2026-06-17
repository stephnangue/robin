package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"

	"github.com/snangue/robin/internal/config"
)

// spiffeFetcher is the production jwtFetcher, backed by the SPIFFE Workload API.
type spiffeFetcher struct {
	src *workloadapi.JWTSource
}

func (f *spiffeFetcher) fetch(ctx context.Context, aud string) (string, time.Time, error) {
	svid, err := f.src.FetchJWTSVID(ctx, jwtsvid.Params{Audience: aud})
	if err != nil {
		return "", time.Time{}, err
	}
	return svid.Marshal(), svid.Expiry, nil
}

func (f *spiffeFetcher) close() error { return f.src.Close() }

// newJWTSVIDSource creates a Workload API JWT source. The socket address is
// passed explicitly when set; otherwise go-spiffe's default applies (which
// honors SPIFFE_ENDPOINT_SOCKET).
func newJWTSVIDSource(ctx context.Context, cfg config.Config) (jwtFetcher, error) {
	var opts []workloadapi.JWTSourceOption
	if cfg.SPIFFESocket != "" {
		opts = append(opts, workloadapi.WithClientOptions(workloadapi.WithAddr(cfg.SPIFFESocket)))
	}
	src, err := workloadapi.NewJWTSource(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("identity/jwtsvid: create source: %w", err)
	}
	return &spiffeFetcher{src: src}, nil
}
