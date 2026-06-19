package identity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileProviderReadAndTrim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  abc.def.ghi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := NewFileProvider(path).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "abc.def.ghi" {
		t.Errorf("token = %q, want trimmed abc.def.ghi", tok)
	}
}

func TestFileProviderRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewFileProvider(path)
	first, _ := p.Token(context.Background())
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil { // simulate kubelet swap
		t.Fatal(err)
	}
	second, _ := p.Token(context.Background())
	if first != "first" || second != "second" {
		t.Errorf("expected per-request re-read, got %q then %q", first, second)
	}
}

func TestFileProviderEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileProvider(path).Token(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Errorf("want ErrNoToken, got %v", err)
	}
}

func TestFileProviderMissing(t *testing.T) {
	p := NewFileProvider(filepath.Join(t.TempDir(), "nope"))
	if _, err := p.Token(context.Background()); err == nil {
		t.Error("want error for missing file")
	}
}
