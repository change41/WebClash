// Package buildinfo holds values injected at link time via -ldflags -X.
package buildinfo

// Version is the release version (overridden by -X webclash/internal/buildinfo.Version=...).
var Version = "0.1.0"

// BuildTime is the unix-millisecond build timestamp used for embedded asset Last-Modified.
var BuildTime = ""
