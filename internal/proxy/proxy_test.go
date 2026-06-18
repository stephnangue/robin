package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snangue/robin/internal/config"
)

type stubProvider struct {
	tok string
	err error
}

func (s stubProvider) Token(context.Context) (string, error) { return s.tok, s.err }
func (s stubProvider) Close() error                          { return nil }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newHandler(t *testing.T, upstream string, p stubProvider) *Handler {
	t.Helper()
	h, err := New(config.Config{UpstreamURL: upstream, TokenSource: "file"}, p, discardLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestInjectOverwritesPlaceholderAndForwardsBody(t *testing.T) {
	var gotAuth, gotBody, gotPath string
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer broker.Close()

	srv := httptest.NewServer(newHandler(t, broker.URL, stubProvider{tok: "realtok"}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat", strings.NewReader("hello-body"))
	req.Header.Set("Authorization", "Bearer placeholder")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotAuth != "Bearer realtok" {
		t.Errorf("Authorization = %q, want Bearer realtok (placeholder overwritten)", gotAuth)
	}
	if gotBody != "hello-body" {
		t.Errorf("forwarded body = %q, want hello-body", gotBody)
	}
	if gotPath != "/v1/chat" {
		t.Errorf("forwarded path = %q, want /v1/chat", gotPath)
	}
}

func TestProviderErrorReturns503(t *testing.T) {
	called := false
	broker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer broker.Close()

	srv := httptest.NewServer(newHandler(t, broker.URL, stubProvider{err: errors.New("boom")}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	if called {
		t.Error("broker must not be contacted when identity is unavailable")
	}
}

func TestEmptyTokenReturns503(t *testing.T) {
	called := false
	broker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer broker.Close()

	// Provider returns ("", nil) — must fail closed, never forward "Bearer ".
	srv := httptest.NewServer(newHandler(t, broker.URL, stubProvider{tok: ""}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 for empty token", resp.StatusCode)
	}
	if called {
		t.Error("broker must not be contacted when the token is empty")
	}
}

func TestBrokerDownReturns502(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // nothing is listening now

	srv := httptest.NewServer(newHandler(t, deadURL, stubProvider{tok: "tok"}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}
