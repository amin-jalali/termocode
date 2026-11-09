//go:build !unix

package selfwatch

import "os"

// Stat is one stat of path: size, time and mode (no inode here).
func Stat(path string) (FileState, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileState{}, err
	}
	return FileState{Size: fi.Size(), Mod: fi.ModTime(), Mode: fi.Mode()}, nil
}
