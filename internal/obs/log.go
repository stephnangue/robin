// Package obs provides observability primitives: structured logging and, in
// v0.2, metrics. The token value is deliberately never a logged field.
package obs

import (
	"log/slog"
	"os"
	"strings"
)

// Structured log field keys, centralized so they stay consistent — and so the
// token value is conspicuously absent from the set.
const (
	FieldProvider       = "provider"
	FieldAudience       = "audience"
	FieldDecision       = "decision"
	FieldUpstreamStatus = "upstream_status"
	FieldError          = "err"
)

// Values for the FieldDecision log field.
const (
	DecisionInjected      = "injected"
	DecisionFailed        = "failed"
	DecisionUpstreamError = "upstream_error"
)

// NewLogger returns a JSON slog.Logger at the given level, writing to stdout.
func NewLogger(level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

// ParseLevel maps a level string to an slog.Level, defaulting to Info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
