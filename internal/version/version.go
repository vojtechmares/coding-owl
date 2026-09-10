// Package version carries the build version of the owl binary.
package version

// Version is set at build time with
// -ldflags "-X github.com/vojtechmares/coding-owl/internal/version.Version=<v>".
// It defaults to "dev" for local builds.
var Version = "dev"
