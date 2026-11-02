// Package version holds build metadata injected with -ldflags.
package version

import "runtime/debug"

// Set at build time:
//
//	-ldflags "-X github.com/digiogithub/hcli/internal/version.Version=v1.2.3
//	          -X github.com/digiogithub/hcli/internal/version.Commit=abc123
//	          -X github.com/digiogithub/hcli/internal/version.Date=2026-11-02T12:00:00Z"
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Get returns the version, falling back to the module version recorded by
// `go install` when nothing was injected.
func Get() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}
