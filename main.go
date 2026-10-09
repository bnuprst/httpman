// Command httpman is a local HTTP client compatible with Postman collections.
// Without arguments it starts the desktop app; `httpman run ...` runs a
// collection from the command line.
package main

import (
	"fmt"
	"os"

	"github.com/bnuprst/httpman/internal/cli"
	"github.com/bnuprst/httpman/internal/httpclient"
)

// version is set with -ldflags "-X main.version=..."
var version = "dev"

func main() {
	cli.Version = version
	httpclient.Version = version
	args := os.Args[1:]
	if cli.IsCLI(args) {
		attachConsole()
		os.Exit(cli.Main(args, os.Stdout, os.Stderr))
	}
	if err := runGUI(args); err != nil {
		attachConsole()
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
