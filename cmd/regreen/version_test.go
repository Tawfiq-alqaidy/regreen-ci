package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	info := func(v string, ok bool) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			if !ok {
				return nil, false
			}
			return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
		}
	}
	tests := []struct {
		name    string
		ldflags string
		read    func() (*debug.BuildInfo, bool)
		want    string
	}{
		{name: "ldflags value wins", ldflags: "v1.2.3", read: info("v9.9.9", true), want: "v1.2.3"},
		{name: "falls back to module version from go install", ldflags: "dev", read: info("v0.4.0", true), want: "v0.4.0"},
		{name: "devel build info is ignored", ldflags: "dev", read: info("(devel)", true), want: "dev"},
		{name: "empty module version is ignored", ldflags: "dev", read: info("", true), want: "dev"},
		{name: "no build info", ldflags: "dev", read: info("", false), want: "dev"},
		{name: "pseudo-version from a local VCS-stamped build is reported", ldflags: "dev", read: info("v0.0.0-20261123151446-f62f1f1a362f+dirty", true), want: "v0.0.0-20261123151446-f62f1f1a362f+dirty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.ldflags, tt.read); got != tt.want {
				t.Errorf("resolveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
