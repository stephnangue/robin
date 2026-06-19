package config

import (
	"strings"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	in := strings.Join([]string{
		"# a comment",
		"",
		"ROBIN_UPSTREAM_URL=https://b.example",
		"export ROBIN_TOKEN_SOURCE=file",
		`ROBIN_AUDIENCE="quoted-aud"`,
		"ROBIN_LISTEN_ADDR='127.0.0.1:4000'",
		"ROBIN_TOKEN_FILE=/var/run/secrets/tokens/token=weird",
	}, "\n")

	m, err := parseDotenv(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ROBIN_UPSTREAM_URL": "https://b.example",
		"ROBIN_TOKEN_SOURCE": "file",
		"ROBIN_AUDIENCE":     "quoted-aud",
		"ROBIN_LISTEN_ADDR":  "127.0.0.1:4000",
		"ROBIN_TOKEN_FILE":   "/var/run/secrets/tokens/token=weird",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
}

func TestParseDotenvError(t *testing.T) {
	if _, err := parseDotenv(strings.NewReader("NOEQUALS")); err == nil {
		t.Error("expected error for line missing '='")
	}
}
