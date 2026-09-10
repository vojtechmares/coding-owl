// Package deploy holds the service unit templates Owl installs on machines
// that did not get it from Homebrew. The daemon never supervises itself
// (ADR-0002), so what is installed is a unit for the platform's own supervisor.
package deploy

import _ "embed"

// LaunchdAgent is the launchd user agent template, rendered by
// internal/launchd.
//
//go:embed launchd/dev.codingowl.owld.plist
var LaunchdAgent string
