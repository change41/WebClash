package util

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
)

// EnsureDir creates dir and parents (port of ensureDir).
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// Exists reports whether a path is accessible (port of exists).
func Exists(file string) bool {
	_, err := os.Stat(file)
	return err == nil
}

// FindInPath locates an executable in PATH (port of findInPath). Returns "" when
// not found. exec.LookPath joins dir+name and checks the executable bit, matching
// the Node access(X_OK) scan on unix.
func FindInPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// ReadFileString reads a whole file as UTF-8 text.
func ReadFileString(file string) (string, error) {
	b, err := os.ReadFile(file)
	return string(b), err
}

// ReadJson reads and parses JSON, returning fallback on any error (port of
// readJson). Non-ENOENT failures are logged to stderr like the Node version.
func ReadJson[T any](file string, fallback T) T {
	raw, err := os.ReadFile(file)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[webclash] 无法读取 %s: %s", file, err)
		}
		return fallback
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Printf("[webclash] 无法读取 %s: %s", file, err)
		return fallback
	}
	return out
}

// WriteJsonAtomic writes 2-space-indented JSON via a temp file + rename.
func WriteJsonAtomic(file string, data any) error {
	b, err := MarshalJSONIndent(data)
	if err != nil {
		return err
	}
	return WriteFileAtomic(file, string(b), nil)
}

// WriteFileAtomic writes data via a unique temp file + rename, optionally
// chmod-ing the temp file first (port of writeFileAtomic). The temp suffix
// includes pid + random bytes so concurrent goroutines never collide.
func WriteFileAtomic(file, data string, mode *os.FileMode) error {
	tmp := fmt.Sprintf("%s.%d.%s.tmp", file, os.Getpid(), RandomToken(4))
	if err := os.WriteFile(tmp, []byte(data), 0o666); err != nil {
		return err
	}
	if mode != nil {
		if err := os.Chmod(tmp, *mode); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// FileMode is a convenience pointer builder for WriteFileAtomic.
func FileMode(m os.FileMode) *os.FileMode { return &m }
