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
	return moveToTrash(s.Path, dest)
}

// moveToTrash keeps the familiar destination when unused and reserves a
// separate directory on collisions, retaining the filename needed for resume.
// The rename itself must reject existing targets:
// checking with Stat first would allow concurrent deletes to lose an archive.
func moveToTrash(source, dest string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if err := renameNoReplace(source, dest); err == nil {
		return dest, nil
	} else if !os.IsExist(err) {
		return "", err
	}
	dir, err := os.MkdirTemp(filepath.Dir(dest), "duplicate-")
	if err != nil {
		return "", err
	}
	dest = filepath.Join(dir, filepath.Base(dest))
	if err := renameNoReplace(source, dest); err != nil {
		_ = os.Remove(dir) // empty reservation only; never remove archive contents
		return "", err
	}
	return dest, nil
}
