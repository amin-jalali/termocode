package webproto

import (
	"encoding/base64"
	"strings"
)

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

// Database limits of the args.
const (
	MaxDBTable = 256
	MaxDBSQL   = 32 << 10
)

// DBConnArgs names a connection by its id.
type DBConnArgs struct {
	Conn string `json:"conn"`
}

// DBRowsArgs pages the rows of a table db.tables listed.
type DBRowsArgs struct {
	Conn  string `json:"conn"`
	Table string `json:"table"`
	From  int    `json:"from,omitempty"`
}

// DBSQLArgs is one SQL statement for a connection.
type DBSQLArgs struct {
	Conn string `json:"conn"`
	SQL  string `json:"sql"`
}

func (a *DBConnArgs) validate(int) error {
	if !validName(a.Conn, MaxID) {
		return invalid("bad connection")
	}
	return nil
}

func (a *DBRowsArgs) validate(int) error {
	if !validName(a.Conn, MaxID) || !validName(a.Table, MaxDBTable) || a.From < 0 || a.From > 1<<30 {
		return invalid("bad connection, table or page")
	}
	return nil
}

func (a *DBSQLArgs) validate(int) error {
	if !validName(a.Conn, MaxID) {
		return invalid("bad connection")
	}
	if strings.TrimSpace(a.SQL) == "" || len(a.SQL) > MaxDBSQL || strings.ContainsRune(a.SQL, 0) {
		return invalid("bad SQL")
	}
	return nil
}

// Transfer limits (WEB-49).
const (
	MaxTransfer   = 256 << 20 // bytes of one file up or down
	TransferChunk = 512 << 10 // bytes of one chunk (before base64)
	MaxUploadID   = 32
)

// DownloadArgs asks for one chunk of a file, from a byte offset.
type DownloadArgs struct {
	Path string `json:"path"`
	From int64  `json:"from,omitempty"`
}

// UploadBeginArgs starts an upload of Size bytes to Path.
type UploadBeginArgs struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// UploadChunkArgs is the next chunk of an upload: Data (base64) holds
// the bytes from Off, which must be what the hub has so far.
type UploadChunkArgs struct {
	ID   string `json:"id"`
	Off  int64  `json:"off"`
	Data string `json:"data"`

	bytes []byte // Data decoded (validate)
}

// Bytes is the decoded chunk.
func (a *UploadChunkArgs) Bytes() []byte { return a.bytes }

// UploadArgs names an upload.
type UploadArgs struct {
	ID string `json:"id"`
}

func validUploadID(id string) bool {
	if len(id) == 0 || len(id) > MaxUploadID {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (a *DownloadArgs) validate(int) error {
	if !validPath(a.Path, false) || a.From < 0 || a.From > MaxTransfer {
		return invalid("bad path or offset")
	}
	return nil
}

func (a *UploadBeginArgs) validate(int) error {
	if !validPath(a.Path, false) || !validFileName(a.Path[strings.LastIndexByte(a.Path, '/')+1:]) {
		return invalid("bad path")
	}
	if a.Size < 0 || a.Size > MaxTransfer {
		return invalid("bad size")
	}
	return nil
}

func (a *UploadChunkArgs) validate(int) error {
	if !validUploadID(a.ID) || a.Off < 0 || a.Off > MaxTransfer {
		return invalid("bad upload or offset")
	}
	b, err := base64.StdEncoding.DecodeString(a.Data)
	if err != nil || len(b) == 0 || len(b) > TransferChunk {
		return invalid("bad chunk")
	}
	a.bytes = b
	return nil
}

func (a *UploadArgs) validate(int) error {
	if !validUploadID(a.ID) {
		return invalid("bad upload")
	}
	return nil
}
