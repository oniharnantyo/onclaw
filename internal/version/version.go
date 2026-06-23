// Package version holds build-time metadata for onclaw, injected via
// -ldflags "-X .../internal/version.<Var>=<value>". See the Makefile and
// .goreleaser.yaml for the exact flags. Defaults make the binary usable
// when built without the release pipeline (e.g. plain `go build`).
package version

var (
	// Version is the semantic version (default "dev").
	Version = "dev"
	// Commit is the short VCS sha (default "none").
	Commit = "none"
	// Date is the build timestamp in UTC RFC3339 (default "unknown").
	Date = "unknown"
)

// String returns a human-readable version line, e.g. "v1.0.0 (abc1234, 2026-06-22T10:00:00Z)".
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
