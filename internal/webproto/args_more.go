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

// MaxBranch caps a branch name; MaxStashMsg a stash message.
const (
	MaxBranch   = 255
	MaxStashMsg = 1 << 10
)

// GitCheckoutArgs switches to a local branch (one git.branches lists).
type GitCheckoutArgs struct {
	Branch string `json:"branch"`
}

// GitStashArgs stashes the changes, with an optional message.
type GitStashArgs struct {
	Message string `json:"message,omitempty"`
}

func (a *GitCheckoutArgs) validate(int) error {
	b := a.Branch
	if b == "" || len(b) > MaxBranch || hasControl(b) || strings.HasPrefix(b, "-") || strings.ContainsAny(b, " ~^:?*[\\") || strings.Contains(b, "..") {
		return invalid("bad branch")
	}
	return nil
}

func (a *GitStashArgs) validate(int) error {
	if len(a.Message) > MaxStashMsg || hasControl(a.Message) {
		return invalid("bad stash message")
	}
	return nil
}

// MaxContainerLogLines caps the lines of one containers.logs.
const MaxContainerLogLines = 1000

// ContainerArgs names a container by its id (as containers.list gives it).
type ContainerArgs struct {
	ID string `json:"id"`
}

// ContainerLogsArgs asks for the last lines of a container's logs.
type ContainerLogsArgs struct {
	ID    string `json:"id"`
	Lines int    `json:"lines,omitempty"` // 0: 200
}

// validContainerID is a hex container id of 12 to 64 characters.
func validContainerID(id string) bool {
	if len(id) < 12 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (a *ContainerArgs) validate(int) error {
	if !validContainerID(a.ID) {
		return invalid("bad container id")
	}
	return nil
}

func (a *ContainerLogsArgs) validate(int) error {
	if !validContainerID(a.ID) || a.Lines < 0 || a.Lines > MaxContainerLogLines {
		return invalid("bad container id or lines")
	}
	return nil
}
