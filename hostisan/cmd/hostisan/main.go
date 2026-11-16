// Command hostisan is the Hostisan control panel server and CLI.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// productURL and securityEmail are HOSTISAN_URL and HOSTISAN_SECURITY_EMAIL of
// install/common/hostisan/product.env, set at build time with -ldflags -X (the
// Makefile's go-build); "" without them, and the UI leaves those links out.
var productURL, securityEmail string

const usage = `usage: hostisan <command>

commands:
  serve     run the panel (see hostisan serve -h)
  terminal  run the web terminal as root (see hostisan terminal -h)
  sessions  end signed-in panel sessions (hostisan sessions forget)
  version   print the version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a subcommand and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		return serve(args[1:], stdout, stderr)
	case "terminal":
		return terminalCmd(args[1:], stdout, stderr)
	case "sessions":
		return sessionsCmd(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "hostisan %s\n", version)
		return 0
	default:
		fmt.Fprintf(stderr, "hostisan: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
