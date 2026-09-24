package store

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(source, dest string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, dest, unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: dest, Err: err}
	}
	return nil
}
