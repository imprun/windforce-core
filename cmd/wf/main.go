package main

import (
	"os"

	"github.com/imprun/windforce-core/internal/controlcli"
)

func main() {
	os.Exit(controlcli.RunWF(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
