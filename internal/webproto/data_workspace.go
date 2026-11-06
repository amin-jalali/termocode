package webproto

import "strings"

// The workspace's folders and the laptop's recent workspaces (WEBP-9,
// Editor and files). The page sees names only, never a path outside
// the workspace; it names a folder or workspace by its place in the
// list. Adding a folder (a path the user types), removing one and
// opening another workspace (the laptop restarts there, which ends this
// web session) wait for a laptop card.

// Op names.
const (
	OpWorkspaceList         = "workspace.list"
	OpWorkspaceAddFolder    = "workspace.add_folder"
	OpWorkspaceRemoveFolder = "workspace.remove_folder"
	OpWorkspaceOpen         = "workspace.open"
)

// MaxFolderPath caps the bytes of a folder path to add.
const MaxFolderPath = 4096

// WorkspacePathArgs names a laptop folder by its absolute path ("~/"
// is the laptop user's home). It is not a workspace path: the laptop
// card shows it whole before anything happens.
type WorkspacePathArgs struct {
	Folder string `json:"folder"`
}

func (a *WorkspacePathArgs) validate(int) error {
	p := a.Folder
	if p == "" || len(p) > MaxFolderPath || hasControl(p) || !(strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~/") || p == "~" || (len(p) > 2 && p[1] == ':' && (p[2] == '\\' || p[2] == '/'))) {
		return invalid("bad folder path")
	}
	return nil
}

// IDArgs names an entry of a list by its ID.
type IDArgs struct {
	ID int `json:"id"`
}

func (a *IDArgs) validate(int) error {
	if a.ID < 0 || a.ID > 1<<20 {
		return invalid("bad id")
	}
	return nil
}

// WorkspaceEntry is a folder or a recent workspace: its place in the
// list and its name.
type WorkspaceEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// WorkspaceList answers workspace.list: the folders (the first is the
// workspace's own, which cannot be removed) and the recent workspaces
// other than this one, newest first.
type WorkspaceList struct {
	Folders []WorkspaceEntry `json:"folders"`
	Recent  []WorkspaceEntry `json:"recent"`
}

var opWorkspaceRows = []Policy{
	{Op: OpWorkspaceList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpWorkspaceAddFolder, New: newOf[WorkspacePathArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 8 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorkspaceRemoveFolder, New: newOf[IDArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWorkspaceOpen, New: newOf[IDArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opWorkspaceRows...); return true }()

func init() {
	registerResult(OpWorkspaceList, WorkspaceList{})
	registerResult(OpWorkspaceAddFolder, AskRef{})
	registerResult(OpWorkspaceRemoveFolder, AskRef{})
	registerResult(OpWorkspaceOpen, AskRef{})
}
