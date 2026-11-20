package main

import (
	"os"

	"webclash/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
