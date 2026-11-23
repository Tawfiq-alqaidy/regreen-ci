package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

// Exit codes returned by run.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usage = `regreen hands mechanical CI failures to your own coding agent
and validates the fix against strict rules before anything is pushed.

Usage:
  regreen <command> [arguments]

Commands:
  mcp       Run the MCP server over stdio for a coding agent
  verify    Check a pushed change against the repository policy (for CI)
  init      Set up regreen.yml, the pre-push hook, and agent permission hints
  watch     Watch CI for manually pushed commits
  version   Print the regreen version
  help      Show this help

Flags:
  -h, --help   Show this help
  --version    Print the regreen version
`

// plannedCommands maps each command that is designed but not built yet to
// the milestone that delivers it. Entries move out of this map as the
// milestones land.
var plannedCommands = map[string]string{
	"mcp":    "M3",
	"verify": "M4",
	"init":   "M4",
	"watch":  "M6",
}

// run executes the CLI and returns the process exit code. It takes its
// arguments and output streams as parameters instead of reading os.Args and
// os.Stdout directly, so tests can call it in-process. Requested output goes
// to stdout; every diagnostic goes to stderr, because "regreen mcp" will
// reserve stdout for the MCP protocol. Write errors are deliberately discarded
// (_, _ =): there is nowhere left to report a failed write to stdout or stderr.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch cmd := args[0]; cmd {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return exitOK
	case "version", "--version":
		_, _ = fmt.Fprintf(stdout, "regreen %s\n", resolveVersion(version, debug.ReadBuildInfo))
		return exitOK
	default:
		if milestone, ok := plannedCommands[cmd]; ok {
			_, _ = fmt.Fprintf(stderr, "regreen %s: not implemented yet (planned for milestone %s)\n", cmd, milestone)
			return exitError
		}
		_, _ = fmt.Fprintf(stderr, "regreen: unknown command %q\nRun 'regreen help' for usage.\n", cmd)
		return exitUsage
	}
}
