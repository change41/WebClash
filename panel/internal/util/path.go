package util

import (
	"path/filepath"
	"strings"
)

// SanitizePath resolves target and verifies it stays inside safeDir. Returns ""
// (the null sentinel) on escape via ../ or an absolute path outside the root.
func SanitizePath(target, safeDir string) string {
	root, err := filepath.Abs(safeDir)
	if err != nil {
		return ""
	}
	resolved, err := filepath.Abs(target)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return ""
	}
	if rel == "." || rel == "" || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
		return resolved
	}
	return ""
}
