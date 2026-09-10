// Command owl is the single Coding Owl binary: daemon, CLI and any future
// Runner behind one cobra entrypoint (ADR-0001).
package main

import (
	"os"

	"github.com/vojtechmares/coding-owl/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
