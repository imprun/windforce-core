package wfcli

import (
	"io"

	"github.com/imprun/windforce-core/internal/controlcli"
)

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return controlcli.RunWFWithCredentialStore(args, stdin, stdout, stderr, credentialStore{})
}
