// Command regreen hands mechanical CI failures to the developer's own coding
// agent and validates the fix against strict rules before anything is pushed.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
