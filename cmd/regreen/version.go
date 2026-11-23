package main

import "runtime/debug"

// devVersion is the placeholder reported by builds that carry no version.
const devVersion = "dev"

// version is set at release build time with:
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/regreen
var version = devVersion

// resolveVersion picks the version to report. A value injected with ldflags
// wins. Otherwise it uses the module version that the Go toolchain embeds
// when the binary is built with "go install module@version". Local builds
// have neither and report "dev".
//
// readBuildInfo is a parameter (in production: debug.ReadBuildInfo) so tests
// can supply fake build information.
func resolveVersion(ldflags string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if ldflags != devVersion {
		return ldflags
	}
	if info, ok := readBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return devVersion
}
