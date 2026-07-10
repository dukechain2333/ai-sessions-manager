package store

import (
	"os"
	"path/filepath"
)

// TrashSession moves a session file into <projectsDir>/.trash/<slug>/
// so deletion is always recoverable with a plain mv. It never removes
// file contents.
func TrashSession(projectsDir string, s Session) (string, error) {
	dest := filepath.Join(projectsDir, ".trash", s.Slug, filepath.Base(s.Path))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(s.Path, dest); err != nil {
		return "", err
	}
	return dest, nil
}
