package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrashSession(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "-home-w-proj", "abc.jsonl")
	writeFile(t, src, "{}\n")
	s := Session{ID: "abc", Path: src, Slug: "-home-w-proj"}

	dest, err := TrashSession(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ".trash", "-home-w-proj", "abc.jsonl")
	if dest != want {
		t.Errorf("dest = %q, want %q", dest, want)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source file still exists")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("trashed file missing: %v", err)
	}
}
