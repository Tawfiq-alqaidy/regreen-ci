package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	// Make the version output independent of how the toolchain stamps the test binary.
	orig := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	t.Cleanup(func() { readBuildInfo = orig })

	tests := []struct {
		name        string
		args        []string
		wantCode    int
		wantStdout  string // exact match when wantUsageOn != "stdout"
		wantStderr  string // exact match when wantUsageOn != "stderr"
		wantUsageOn string // "stdout", "stderr", or ""
	}{
		{name: "no args prints usage to stderr", args: nil, wantCode: 2, wantUsageOn: "stderr"},
		{name: "help command", args: []string{"help"}, wantCode: 0, wantUsageOn: "stdout"},
		{name: "-h flag", args: []string{"-h"}, wantCode: 0, wantUsageOn: "stdout"},
		{name: "--help flag", args: []string{"--help"}, wantCode: 0, wantUsageOn: "stdout"},
		{name: "version command", args: []string{"version"}, wantCode: 0, wantStdout: "regreen dev\n"},
		{name: "--version flag", args: []string{"--version"}, wantCode: 0, wantStdout: "regreen dev\n"},
		{
			name: "mcp is planned", args: []string{"mcp"}, wantCode: 1,
			wantStderr: "regreen mcp: not implemented yet (planned for milestone M3)\n",
		},
		{
			name: "verify is planned", args: []string{"verify"}, wantCode: 1,
			wantStderr: "regreen verify: not implemented yet (planned for milestone M4)\n",
		},
		{
			name: "init is planned", args: []string{"init"}, wantCode: 1,
			wantStderr: "regreen init: not implemented yet (planned for milestone M4)\n",
		},
		{
			name: "watch is planned", args: []string{"watch"}, wantCode: 1,
			wantStderr: "regreen watch: not implemented yet (planned for milestone M6)\n",
		},
		{
			name: "unknown command", args: []string{"bogus"}, wantCode: 2,
			wantStderr: "regreen: unknown command \"bogus\"\nRun 'regreen help' for usage.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			wantStdout, wantStderr := tt.wantStdout, tt.wantStderr
			switch tt.wantUsageOn {
			case "stdout":
				wantStdout = usage
			case "stderr":
				wantStderr = usage
			}
			if got := stdout.String(); got != wantStdout {
				t.Errorf("stdout = %q, want %q", got, wantStdout)
			}
			if got := stderr.String(); got != wantStderr {
				t.Errorf("stderr = %q, want %q", got, wantStderr)
			}
		})
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	for name := range plannedCommands {
		if !strings.Contains(usage, "\n  "+name+" ") {
			t.Errorf("usage does not list planned command %q", name)
		}
	}
	for _, name := range []string{"version", "help"} {
		if !strings.Contains(usage, "\n  "+name+" ") {
			t.Errorf("usage does not list command %q", name)
		}
	}
}
