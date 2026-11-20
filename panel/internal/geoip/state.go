package geoip

import (
	"path/filepath"

	"webclash/internal/util"
)

// FileMeta is the persisted download fingerprint of one database file. ETag /
// LastModified come from the remote response (empty when a mirror strips them);
// Size / UpdatedAt describe the local copy. Version is filled by the reader.
type FileMeta struct {
	ETag         string `json:"etag"`
	LastModified string `json:"lastModified"`
	Size         int64  `json:"size"`
	UpdatedAt    int64  `json:"updatedAt"`
	Version      string `json:"version,omitempty"`
}

// State is the on-disk geoip/state.json snapshot. It is additive: an absent or
// corrupt file degrades to an empty state, never a fatal error.
type State struct {
	Source          string              `json:"source"`
	Files           map[string]FileMeta `json:"files"`
	LastCheckAt     *int64              `json:"lastCheckAt"`
	UpdateAvailable bool                `json:"updateAvailable"`
}

func statePath(dir string) string { return filepath.Join(dir, "state.json") }

func loadState(dir string) State {
	s := util.ReadJson[State](statePath(dir), State{})
	if s.Files == nil {
		s.Files = map[string]FileMeta{}
	}
	return s
}

func saveState(dir string, s State) error {
	if s.Files == nil {
		s.Files = map[string]FileMeta{}
	}
	return util.WriteJsonAtomic(statePath(dir), s)
}
