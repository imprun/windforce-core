package webui

import "embed"

// FS contains the windforce-core Web UI static assets.
//
//go:embed all:assets
var FS embed.FS
