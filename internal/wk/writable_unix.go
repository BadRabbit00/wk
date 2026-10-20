//go:build !windows

package wk

import (
	"path/filepath"
	"syscall"
)

const installHint = "установи пакет wk в /usr/bin"

func userWritable(path string) bool {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	const writeOK = 2 // W_OK: replacing needs write access to the file or its directory.
	return syscall.Access(path, writeOK) == nil || syscall.Access(filepath.Dir(path), writeOK) == nil
}
