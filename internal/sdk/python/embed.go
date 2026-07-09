// Package windforcepyclient embeds the vendored Python SDK so the worker can
// inject it into each commit's python vendor dir (resolving "windforce_client"
// for user scripts). Mirrors sdk/typescript/embed.go.
//
// The `all:` prefix is required because go:embed otherwise skips names beginning
// with "_" or "." — and the package's __init__.py starts with "_".
package windforcepyclient

import "embed"

//go:embed all:windforce_client
var Files embed.FS
