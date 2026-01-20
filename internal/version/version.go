// Package version holds the build version for agency.
package version

// Version is set at build time via -ldflags.
var Version = "dev"

// Commit is the git commit SHA, set at build time via -ldflags.
var Commit = ""

// FullVersion returns the version string with commit if available.
// Format: "vX.Y.Z (commit <shortsha>)" or "dev" for dev builds.
func FullVersion() string {
	if Commit != "" {
		return Version + " (commit " + Commit + ")"
	}
	return Version
}
