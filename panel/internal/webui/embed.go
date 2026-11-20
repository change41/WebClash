// Package webui embeds the built React frontend and serves it with the same
// cache/ETag/SPA semantics as the Node static-embedded layer.
package webui

import "embed"

// dist is populated at release build time by copying web/dist here. A minimal
// placeholder index.html is committed so the package compiles before that copy.
//
//go:embed all:dist
var distFS embed.FS
