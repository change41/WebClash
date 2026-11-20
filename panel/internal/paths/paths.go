// Package paths resolves data directory, executable path and web asset overrides.
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// Embedded reports whether the frontend is embedded in the binary. The Go
// release is always a single static binary with go:embed assets.
func Embedded() bool { return true }

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

// ExpandHome expands a leading ~ to the user home directory.
func ExpandHome(target string) string {
	if target == "~" {
		return homeDir()
	}
	if strings.HasPrefix(target, "~/") {
		return filepath.Join(homeDir(), target[2:])
	}
	return target
}

// DefaultDataDir honors DATA_DIR, otherwise ~/.webclash for the embedded form.
func DefaultDataDir() string {
	if v := strings.TrimSpace(os.Getenv("DATA_DIR")); v != "" {
		return ExpandHome(v)
	}
	return filepath.Join(homeDir(), ".webclash")
}

// WebDistDir returns the disk frontend override (WEBCLASH_WEB_DIST), or "" to
// use the embedded assets.
func WebDistDir() string {
	return strings.TrimSpace(os.Getenv("WEBCLASH_WEB_DIST"))
}

// ExecutablePath returns the absolute path of the running binary.
func ExecutablePath() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return os.Args[0]
}
