// This file also embeds the SDK's own source + module file so the worker can inject
// it into each commit as the windforce-client module, resolved via a go.mod replace
// (ADR-0040; mirrors the sdk/typescript and sdk/python embeds). The embedded
// windforce.go compiles into the worker too (an unused package), which is harmless.
package windforceclient

import "embed"

// Files holds the SDK source (windforce.go) + the injected module's go.mod
// (gomod.txt). injectGoSDK writes both into <commit>/.windforce/sdk-go/.
//
//go:embed windforce.go gomod.txt
var Files embed.FS
