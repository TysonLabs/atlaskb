package version

var (
	// Version is the semantic version (without leading "v") set at build time.
	Version = "dev"
	// Commit is the git commit hash set at build time.
	Commit = "none"
	// Date is the build date in RFC3339 format set at build time.
	Date = "unknown"
)
