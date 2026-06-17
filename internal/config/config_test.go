package config

import (
	"os"
	"path/filepath"
	"testing"
)

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "robin.env")
	if err := os.WriteFile(envFile, []byte("ROBIN_UPSTREAM_URL=https://file.example\nROBIN_TOKEN_SOURCE=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"file only", nil, map[string]string{"ROBIN_CONFIG": envFile}, "https://file.example"},
		{"env over file", nil, map[string]string{"ROBIN_CONFIG": envFile, "ROBIN_UPSTREAM_URL": "https://env.example"}, "https://env.example"},
		{"flag over env", []string{"--upstream-url=https://flag.example"}, map[string]string{"ROBIN_CONFIG": envFile, "ROBIN_UPSTREAM_URL": "https://env.example"}, "https://flag.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(tt.args, mapEnv(tt.env))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.UpstreamURL != tt.want {
				t.Errorf("UpstreamURL = %q, want %q", cfg.UpstreamURL, tt.want)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil, mapEnv(map[string]string{"ROBIN_UPSTREAM_URL": "https://b.example"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TokenSource != "file" || cfg.ListenAddr != ":4000" || cfg.AdminAddr != ":4001" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.SVIDRefreshBefore.Seconds() != 60 {
		t.Errorf("SVIDRefreshBefore = %v, want 60s", cfg.SVIDRefreshBefore)
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"missing upstream", nil, true},
		{"bad upstream url", map[string]string{"ROBIN_UPSTREAM_URL": "not-a-url"}, true},
		{"jwtsvid without audience", map[string]string{"ROBIN_UPSTREAM_URL": "https://b", "ROBIN_TOKEN_SOURCE": "jwtsvid"}, true},
		{"unknown token source", map[string]string{"ROBIN_UPSTREAM_URL": "https://b", "ROBIN_TOKEN_SOURCE": "bogus"}, true},
		{"valid file", map[string]string{"ROBIN_UPSTREAM_URL": "https://b.example"}, false},
		{"valid jwtsvid", map[string]string{"ROBIN_UPSTREAM_URL": "https://b.example", "ROBIN_TOKEN_SOURCE": "jwtsvid", "ROBIN_AUDIENCE": "aud"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(nil, mapEnv(tt.env))
			if (err != nil) != tt.wantErr {
				t.Errorf("Load err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseUIDs(t *testing.T) {
	got, err := parseUIDs(" 1000, 1001 ,1002")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 1000 || got[2] != 1002 {
		t.Errorf("parseUIDs = %v", got)
	}
	if _, err := parseUIDs("abc"); err == nil {
		t.Error("expected error for non-numeric uid")
	}
	if got, _ := parseUIDs(""); got != nil {
		t.Errorf("empty should be nil, got %v", got)
	}
}

func TestPeerCredRequiresUDS(t *testing.T) {
	_, err := Load(nil, mapEnv(map[string]string{
		"ROBIN_UPSTREAM_URL":        "https://b.example",
		"ROBIN_PEERCRED_ALLOW_UIDS": "1000",
	}))
	if err == nil {
		t.Error("expected error: peercred uids without UDS")
	}
}
