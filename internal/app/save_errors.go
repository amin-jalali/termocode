package app

import (
	"os"
	"path/filepath"
	"strings"
)

// saveFailText turns the error of a nvim :write into short words for the
// user. nvim says "nvim:nvim_command exception: Vim(write):E45: 'readonly'
// option is set (add ! to override)", which tells a user nothing about
// what to do; the text is still in the editor, and the message says so.
func saveFailText(path string, err error) string {
	msg := err.Error()
	if strings.Contains(msg, "termocode: ") {
		return saveErrText(err) // our own sentence (a remote buffer save)
	}
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || path == "" {
		name = "The file"
	}
	const kept = " Your changes are still in the editor."
	switch {
	case strings.Contains(msg, "E45:") || strings.Contains(msg, "E505:"):
		return name + " is read-only." + kept
	case strings.Contains(msg, "E514:"):
		return "The disk is full: " + name + " was not saved." + kept
	case strings.Contains(msg, "E212:") || strings.Contains(msg, "permission denied"):
		if dir := filepath.Dir(path); path != "" && !dirWritable(dir) {
			return "No permission to write in folder " + filepath.Base(dir) + "." + kept
		}
		return "No permission to write " + name + "." + kept
	}
	// Anything else: nvim's own words without the RPC prefix.
	if i := strings.Index(msg, "Vim(write):"); i >= 0 {
		msg = msg[i+len("Vim(write):"):]
	} else if i := strings.Index(msg, "exception: "); i >= 0 {
		msg = msg[i+len("exception: "):]
	}
	return strings.TrimSpace(msg) + "." + kept
}

// dirWritable reports whether a new file can be made in dir.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".termocode-w-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
