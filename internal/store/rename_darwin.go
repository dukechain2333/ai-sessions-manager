package store

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(source, dest string) error {
	if err := unix.RenamexNp(source, dest, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: dest, Err: err}
	}
	return nil
}
