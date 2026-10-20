package wk

import (
	"os"
	"path/filepath"
)

const installHint = `положи wk.exe в каталог, куда пишет только администратор, например C:\Program Files\wk`

// Windows has no access(2), and its ACLs decide what the read-only attribute
// does not, so the directory is probed by creating a file in it: whoever can
// do that can rename the running wk.exe aside and put another one in its place.
func userWritable(path string) bool {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	probe, err := os.CreateTemp(filepath.Dir(path), ".wk-probe-*")
	if err != nil {
		return false
	}
	probe.Close()
	os.Remove(probe.Name())
	return true
}
