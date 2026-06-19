package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/snangue/robin/internal/config"
	"github.com/snangue/robin/internal/proxy"
)

type stubProvider struct {
	tok string
	err error
}

func (s stubProvider) Token(context.Context) (string, error) { return s.tok, s.err }
func (s stubProvider) Close() error                          { return nil }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func startServer(t *testing.T, broker string, p stubProvider) *Server {
	t.Helper()
	cfg := config.Config{UpstreamURL: broker, TokenSource: "file", ListenAddr: "127.0.0.1:0", AdminAddr: "127.0.0.1:0"}
	h, err := proxy.New(cfg, p, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(cfg, h, AdminMux(p), discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestHealthReadyAndProxy(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Auth", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer broker.Close()

	srv := startServer(t, broker.URL, stubProvider{tok: "tok"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	get := func(url string) *http.Response {
		t.Helper()
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	if r := get("http://" + srv.AdminAddr().String() + "/healthz"); r.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", r.StatusCode)
		r.Body.Close()
	} else {
		r.Body.Close()
	}

	if r := get("http://" + srv.AdminAddr().String() + "/readyz"); r.StatusCode != http.StatusOK {
		t.Errorf("/readyz = %d, want 200", r.StatusCode)
		r.Body.Close()
	} else {
		r.Body.Close()
	}

	r := get("http://" + srv.ProxyAddr().String() + "/v1/x")
	if got := r.Header.Get("X-Auth"); got != "Bearer tok" {
		t.Errorf("proxied Authorization = %q, want Bearer tok", got)
	}
	r.Body.Close()

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned %v", err)
	}
}

func TestReadyzReflectsProviderFailure(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer broker.Close()

	srv := startServer(t, broker.URL, stubProvider{err: errors.New("workload api down")})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	resp, err := http.Get("http://" + srv.AdminAddr().String() + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/readyz = %d, want 503 when provider cannot resolve", resp.StatusCode)
	}
	resp.Body.Close()

	cancel()
	<-done
}

func TestGracefulShutdownDrainsInflight(t *testing.T) {
	received := make(chan struct{})
	release := make(chan struct{})
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(received)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer broker.Close()

	srv := startServer(t, broker.URL, stubProvider{tok: "tok"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	addr := srv.ProxyAddr().String()
	codes := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			codes <- -1
			return
		}
		codes <- resp.StatusCode
		resp.Body.Close()
	}()

	<-received     // request is in-flight at the broker
	cancel()       // SIGTERM-equivalent: begin graceful shutdown
	close(release) // let the broker finish responding

	if code := <-codes; code != http.StatusOK {
		t.Errorf("in-flight request got %d, want 200 (should drain, not be severed)", code)
	}
	if err := <-done; err != nil {
		t.Errorf("Run returned %v", err)
	}
}
