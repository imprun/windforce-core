package main

import (
	"os"

	"github.com/imprun/windforce-core/internal/wfcli"
)

func main() {
	os.Exit(wfcli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
