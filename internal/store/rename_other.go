//go:build !darwin && !linux

package store

import "errors"

// Fail without changing either file on platforms without the no-replace
// rename implementation. Releases target macOS and Linux.
func renameNoReplace(source, dest string) error {
	return errors.New("safe trash is unsupported on this platform")
}
