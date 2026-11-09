//go:build unix

package selfwatch

import (
	"os"
	"syscall"
)

// Stat is one stat of path (it follows a symlink: the file that runs).
func Stat(path string) (FileState, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileState{}, err
	}
	st := FileState{Size: fi.Size(), Mod: fi.ModTime(), Mode: fi.Mode()}
	if sys, ok := fi.Sys().(*syscall.Stat_t); ok {
		st.Dev, st.Ino = uint64(sys.Dev), uint64(sys.Ino) //nolint:unconvert // int32 on some systems
	}
	return st, nil
}
