// Command commhub is a terminal dashboard for your mail, calendar and meetings.
package main

import (
	"os"

	"github.com/Neha611/commhub/internal/cli"
)

func main() {
	hardenProcess()
	os.Exit(cli.Run(os.Args[1:]))
}
