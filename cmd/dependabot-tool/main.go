package main

import (
	"fmt"
	"os"

	"github.com/iyuuya/dependabot-tool/internal/alertscmd"
	"github.com/iyuuya/dependabot-tool/internal/historycmd"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: dependabot-tool <alerts|history> [flags]")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	sub := os.Args[1]
	rest := os.Args[2:]

	var err error
	switch sub {
	case "alerts":
		err = alertscmd.Run(rest)
	case "history", "histories":
		err = historycmd.Run(rest)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", sub)
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
