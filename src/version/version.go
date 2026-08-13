// Package version holds build-time version information injected via ldflags.
package version

var (
	// Version is the semantic version, set via -ldflags at build time.
	Version = "dev"
	// GitCommit is the git SHA, set via -ldflags at build time.
	GitCommit = "unknown"
	// BuildDate is the ISO 8601 build timestamp, set via -ldflags at build time.
	BuildDate = "unknown"
)
