package version

// These values are overridden with -ldflags for release images.
var (
	Version = "2.0.0-dev"
	Commit  = "worktree"
	BuiltAt = "unknown"
)
