// Package version holds build metadata injected at link time via -ldflags.
package version

// Build metadata, set via -X linker flags (see Makefile). Defaults apply to
// `go run` and `go test` builds.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return Version + " (commit " + Commit + ", built " + Date + ")"
}
