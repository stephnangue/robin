// Package config loads Robin's flat ROBIN_* configuration from flags,
// environment, and an optional .env-style file (precedence: flags > env > file).
package config

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/snangue/robin/internal/obs"
)

// Config is Robin's fully-resolved configuration.
type Config struct {
	UpstreamURL       string        // ROBIN_UPSTREAM_URL (required) — broker base URL
	TokenSource       string        // ROBIN_TOKEN_SOURCE — file | jwtsvid
	ListenAddr        string        // ROBIN_LISTEN_ADDR — proxy listener
	ListenUDS         string        // ROBIN_LISTEN_UDS — UDS path (enables peer-cred mode)
	AdminAddr         string        // ROBIN_ADMIN_ADDR — admin/health/metrics listener
	TokenFile         string        // ROBIN_TOKEN_FILE — file provider token path
	Audience          string        // ROBIN_AUDIENCE — required for jwtsvid
	SPIFFESocket      string        // ROBIN_SPIFFE_SOCKET — optional Workload API socket addr
	SVIDRefreshBefore time.Duration // ROBIN_SVID_REFRESH_BEFORE — refresh lead before exp
	UpstreamCAFile    string        // ROBIN_UPSTREAM_CA_FILE — CA to verify broker TLS
	PeerCredAllowUIDs []int         // ROBIN_PEERCRED_ALLOW_UIDS — allowed peer UIDs (UDS mode)
	LogLevel          slog.Level    // ROBIN_LOG_LEVEL
}

// Load resolves configuration with precedence flags > env > file. getenv and
// args are injected so precedence is testable without touching process state.
func Load(args []string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	path := configPath(args, getenv)
	fileMap := map[string]string{}
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("config: open %s: %w", path, err)
		}
		defer f.Close()
		fileMap, err = parseDotenv(f)
		if err != nil {
			return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}

	// val resolves a key by env first, then file, then the provided default.
	val := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		if v, ok := fileMap[key]; ok && v != "" {
			return v
		}
		return def
	}

	var (
		cfg                          Config
		refreshStr, uidStr, levelStr string
		cfgPath                      string
	)

	fs := flag.NewFlagSet("robin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	// --config is resolved before flag parsing (see configPath) so the file is
	// already loaded; it is registered here only so Parse accepts the flag.
	fs.StringVar(&cfgPath, "config", path, "path to a flat KEY=value config file")
	fs.StringVar(&cfg.UpstreamURL, "upstream-url", val("ROBIN_UPSTREAM_URL", ""), "broker base URL (required)")
	fs.StringVar(&cfg.TokenSource, "token-source", val("ROBIN_TOKEN_SOURCE", "file"), "identity source: file|jwtsvid")
	fs.StringVar(&cfg.ListenAddr, "listen-addr", val("ROBIN_LISTEN_ADDR", "127.0.0.1:4000"), "proxy listen address")
	fs.StringVar(&cfg.ListenUDS, "listen-uds", val("ROBIN_LISTEN_UDS", ""), "proxy UDS path (enables peer-cred mode)")
	fs.StringVar(&cfg.AdminAddr, "admin-addr", val("ROBIN_ADMIN_ADDR", ":4001"), "admin listen address")
	fs.StringVar(&cfg.TokenFile, "token-file", val("ROBIN_TOKEN_FILE", "/var/run/secrets/tokens/token"), "file provider token path")
	fs.StringVar(&cfg.Audience, "audience", val("ROBIN_AUDIENCE", ""), "token audience (required for jwtsvid)")
	fs.StringVar(&cfg.SPIFFESocket, "spiffe-socket", val("ROBIN_SPIFFE_SOCKET", ""), "SPIFFE Workload API socket address")
	fs.StringVar(&refreshStr, "svid-refresh-before", val("ROBIN_SVID_REFRESH_BEFORE", "60s"), "refresh SVID this long before expiry")
	fs.StringVar(&cfg.UpstreamCAFile, "upstream-ca-file", val("ROBIN_UPSTREAM_CA_FILE", ""), "CA file to verify broker TLS")
	fs.StringVar(&uidStr, "peercred-allow-uids", val("ROBIN_PEERCRED_ALLOW_UIDS", ""), "comma-separated allowed peer UIDs")
	fs.StringVar(&levelStr, "log-level", val("ROBIN_LOG_LEVEL", "info"), "log level: debug|info|warn|error")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	d, err := time.ParseDuration(refreshStr)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid svid-refresh-before %q: %w", refreshStr, err)
	}
	cfg.SVIDRefreshBefore = d

	uids, err := parseUIDs(uidStr)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid peercred-allow-uids %q: %w", uidStr, err)
	}
	cfg.PeerCredAllowUIDs = uids

	cfg.LogLevel = obs.ParseLevel(levelStr)

	return cfg, cfg.Validate()
}

// Validate enforces fail-closed configuration rules.
func (c Config) Validate() error {
	if c.UpstreamURL == "" {
		return fmt.Errorf("config: ROBIN_UPSTREAM_URL is required")
	}
	if u, err := url.Parse(c.UpstreamURL); err != nil || !u.IsAbs() || u.Host == "" {
		return fmt.Errorf("config: ROBIN_UPSTREAM_URL %q must be an absolute URL", c.UpstreamURL)
	}
	switch c.TokenSource {
	case "file":
		// Token file is read at request time; nothing else required here.
	case "jwtsvid":
		if c.Audience == "" {
			return fmt.Errorf("config: ROBIN_AUDIENCE is required for token-source=jwtsvid")
		}
	default:
		return fmt.Errorf("config: ROBIN_TOKEN_SOURCE %q must be file or jwtsvid", c.TokenSource)
	}
	if c.SVIDRefreshBefore <= 0 {
		return fmt.Errorf("config: ROBIN_SVID_REFRESH_BEFORE must be positive")
	}
	if len(c.PeerCredAllowUIDs) > 0 && c.ListenUDS == "" {
		return fmt.Errorf("config: ROBIN_PEERCRED_ALLOW_UIDS set but ROBIN_LISTEN_UDS is empty")
	}
	return nil
}

// configPath resolves the config-file path from a --config flag or ROBIN_CONFIG.
func configPath(args []string, getenv func(string) string) string {
	for i, a := range args {
		switch {
		case a == "--config" || a == "-config":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--config="):
			return strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-config="):
			return strings.TrimPrefix(a, "-config=")
		}
	}
	return getenv("ROBIN_CONFIG")
}

func parseUIDs(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var uids []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid uid", p)
		}
		uids = append(uids, n)
	}
	return uids, nil
}
