package main

import (
	"fmt"
	"os"

	"codex-portal/internal/portal"
)

func main() {
	if err := portal.RunCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
