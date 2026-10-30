package webproto

import "strings"

// Args of the v2 view ops (WEB-41 … WEB-49) and their wire rules. The
// hub still resolves every path inside the workspace.

// MaxName caps one file or folder name.
const MaxName = 255

// ItemArgs names one file or folder that is not the workspace root (a
// new one for tree.new_file and tree.new_dir).
type ItemArgs struct {
	Path string `json:"path"`
}

// RenameArgs gives a file or folder a new name in the same folder.
type RenameArgs struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// validFileName is one plain name: no slash or backslash, not "." or
// "..", no control character.
func validFileName(n string) bool {
	return n != "" && n != "." && n != ".." && len(n) <= MaxName && !hasControl(n) && !strings.ContainsAny(n, `/\`)
}

func (a *ItemArgs) validate(int) error {
	if !validPath(a.Path, false) {
		return invalid("bad path")
	}
	return nil
}

func (a *RenameArgs) validate(int) error {
	if !validPath(a.Path, false) {
		return invalid("bad path")
	}
	if !validFileName(a.Name) {
		return invalid("bad name")
	}
	return nil
}
