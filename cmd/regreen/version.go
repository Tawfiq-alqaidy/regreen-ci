package main

import "runtime/debug"

// devVersion is the placeholder reported by builds that carry no version.
const devVersion = "dev"

// version is set at release build time with:
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/regreen
var version = devVersion

// resolveVersion picks the version to report. A value injected with ldflags
// wins, unless it is empty or the "dev" placeholder, which both count as
// unset. Otherwise it uses the main module version from the Go build info:
// the release tag for "go install module@version", or a pseudo-version such
// as v0.0.0-<date>-<commit>+dirty for a plain "go build" inside a git
// checkout (the toolchain stamps the VCS state). "dev" is reported only when
// there is no usable build info: none at all, an empty version, or "(devel)".
//
// readBuildInfo is a parameter (in production: debug.ReadBuildInfo) so tests
// can supply fake build information.
func resolveVersion(ldflags string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if ldflags != "" && ldflags != devVersion {
		return ldflags
	}
	if info, ok := readBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return devVersion
}
