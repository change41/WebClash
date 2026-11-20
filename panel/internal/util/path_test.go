package util

import (
	"path/filepath"
	"testing"
)

func TestSanitizePath(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name   string
		target string
		want   string
	}{
		{"inside", filepath.Join(root, "a", "b"), filepath.Join(root, "a", "b")},
		{"root itself", root, root},
		{"inner dotdot stays", filepath.Join(root, "a", "..", "b"), filepath.Join(root, "b")},
		{"escape", filepath.Join(root, "..", "escape"), ""},
		{"absolute outside", "/etc/passwd", ""},
	}
	for _, c := range cases {
		if got := SanitizePath(c.target, root); got != c.want {
			t.Errorf("%s: SanitizePath(%q, root) = %q, want %q", c.name, c.target, got, c.want)
		}
	}
}
