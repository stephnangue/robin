package identity

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// FileProvider serves a Kubernetes projected ServiceAccount token from a file.
// The kubelet atomically rotates the file in place (~80% TTL), so the provider
// re-reads it on every request and never caches.
type FileProvider struct {
	path string
}

// NewFileProvider returns a FileProvider reading the token at path.
func NewFileProvider(path string) *FileProvider {
	return &FileProvider{path: path}
}

// Token reads, trims, and returns the token file contents.
func (p *FileProvider) Token(_ context.Context) (string, error) {
	b, err := os.ReadFile(p.path)
	if err != nil {
		return "", fmt.Errorf("identity/file: read %s: %w", p.path, err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", ErrNoToken
	}
	return tok, nil
}

// Close is a no-op; the file provider holds no resources.
func (p *FileProvider) Close() error { return nil }
