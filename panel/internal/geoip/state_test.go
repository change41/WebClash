package geoip

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := int64(1_700_000_000_000)
	s := State{
		Source:          "ip2region",
		Files:           map[string]FileMeta{fileV4: {ETag: "a", Size: 10, UpdatedAt: 5, Version: "2024-01-01"}},
		LastCheckAt:     &now,
		UpdateAvailable: true,
	}
	if err := saveState(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadState(dir)
	if got.Source != "ip2region" || !got.UpdateAvailable {
		t.Fatalf("source/flag lost: %+v", got)
	}
	if got.LastCheckAt == nil || *got.LastCheckAt != now {
		t.Fatalf("lastCheckAt lost: %+v", got.LastCheckAt)
	}
	fm := got.Files[fileV4]
	if fm.ETag != "a" || fm.Size != 10 || fm.UpdatedAt != 5 || fm.Version != "2024-01-01" {
		t.Fatalf("file meta lost: %+v", fm)
	}
}

func TestLoadStateAbsent(t *testing.T) {
	got := loadState(t.TempDir())
	if got.Files == nil {
		t.Fatal("absent state must yield a non-nil Files map")
	}
	if got.Source != "" || got.UpdateAvailable || got.LastCheckAt != nil {
		t.Fatalf("absent state must be empty: %+v", got)
	}
}

func TestLoadStateNilFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(statePath(dir), []byte(`{"source":"mmdb"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadState(dir)
	if got.Files == nil {
		t.Fatal("nil files in JSON must normalize to a non-nil map")
	}
	if got.Source != "mmdb" {
		t.Fatalf("source = %q", got.Source)
	}
}

func TestSaveStateNilFiles(t *testing.T) {
	dir := t.TempDir()
	if err := saveState(dir, State{Source: "mmdb"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := loadState(dir); got.Files == nil {
		t.Fatal("saved nil files must reload as a non-nil map")
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatalf("state.json not written: %v", err)
	}
}
