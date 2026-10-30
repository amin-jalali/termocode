package webproto

// Topic data of the file views (web-ui-design §2.3, §2.4): the tree, the
// laptop's buffers and one buffer's lines. Paths are workspace-relative
// with forward slashes ("" is the root). Rows and columns are zero-based;
// a column counts bytes, as nvim does. Tick is nvim's changedtick.

// ---- tree ----

// TreeSnap is the "tree" snapshot: the roots and every folder some
// browser listed (tree.list). Hidden says the laptop shows dotfiles.
type TreeSnap struct {
	Roots  []TreeRoot         `json:"roots"`
	Hidden bool               `json:"hidden"`
	Dirs   map[string]TreeDir `json:"dirs"`
}

// TreeRoot is one root of the workspace.
type TreeRoot struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

// TreeDir is the listing of one folder: folders first, then by name.
// Truncated: there were more than MaxTreeEntries.
type TreeDir struct {
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// TreeEntry is one name in a folder. Mtime is Unix ms. Git is the
// status letter the explorer shows ("M", "A", "D", "R", "?" for
// untracked, "" for clean). Ignored entries are drawn dimmed, as on the
// laptop.
type TreeEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Mtime   int64  `json:"mtime"`
	Git     string `json:"git,omitempty"`
	Hidden  bool   `json:"hidden,omitempty"`
	Ignored bool   `json:"ignored,omitempty"`
}

// TreeDirDelta replaces the listing of Path.
type TreeDirDelta struct {
	Kind      string      `json:"kind" const:"dir"`
	Path      string      `json:"path"`
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// TreeRemoveDelta drops Path and every folder under it: the folder is
// gone, or no browser lists it any more.
type TreeRemoveDelta struct {
	Kind string `json:"kind" const:"remove"`
	Path string `json:"path"`
}

// TreeHiddenDelta: the laptop showed or hid dotfiles (the listed
// folders follow as dir deltas).
type TreeHiddenDelta struct {
	Kind string `json:"kind" const:"hidden"`
	On   bool   `json:"on"`
}

// TreeListRes is the result of tree.list.
type TreeListRes struct {
	Path      string      `json:"path"`
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated,omitempty"`
}

// MaxTreeEntries caps one folder listing.
const MaxTreeEntries = 2000

// MaxTreeDirs caps the folders one connection lists at once.
const MaxTreeDirs = 64

// ---- bufs ----

// BufsSnap is the "bufs" snapshot and its only delta (the whole list):
// the files open on the laptop, in its tab order.
type BufsSnap struct {
	Bufs []BufItem `json:"bufs"`
}

// BufItem is one buffer open on the laptop.
type BufItem struct {
	Buf    int    `json:"buf"`
	Path   string `json:"path"`
	Dirty  bool   `json:"dirty"`
	Active bool   `json:"active"`
}

// ---- buf:<id> ----

// BufSnap is the "buf:<id>" snapshot: the whole buffer.
type BufSnap struct {
	Buf      int      `json:"buf"`
	Path     string   `json:"path"`
	Tick     int      `json:"tick"`
	Lines    []string `json:"lines"`
	Dirty    bool     `json:"dirty"`
	Ft       string   `json:"ft"`
	Readonly bool     `json:"readonly"`
	// The buffer's layout, as the laptop's editor has it: tab width,
	// spaces for tabs, the file's encoding ("" is utf-8) and line ends
	// ("unix", "dos", "mac"). The page draws tabs that wide and shows them
	// in its status bar.
	Tabstop   int    `json:"ts,omitempty"`
	Expandtab bool   `json:"et,omitempty"`
	Enc       string `json:"enc,omitempty"`
	Eol       string `json:"eol,omitempty"`
}

// LinesDelta replaces rows [First, Last) with Lines (as
// nvim_buf_set_lines); Tick is the changedtick after it. Terminals send
// the same delta with absolute buffer rows.
type LinesDelta struct {
	Kind  string   `json:"kind" const:"lines"`
	First int      `json:"first"`
	Last  int      `json:"last"`
	Lines []string `json:"lines"`
	Tick  int      `json:"tick"`
}

// TickDelta: the changedtick moved without a text change.
type TickDelta struct {
	Kind string `json:"kind" const:"tick"`
	Tick int    `json:"tick"`
}

// DirtyDelta: the buffer turned modified or saved.
type DirtyDelta struct {
	Kind string `json:"kind" const:"dirty"`
	On   bool   `json:"on"`
}

// ResetDelta replaces the whole buffer (after a reload from disk, or
// when the hub lost track of a change).
type ResetDelta struct {
	Kind  string   `json:"kind" const:"reset"`
	Tick  int      `json:"tick"`
	Lines []string `json:"lines"`
	Dirty bool     `json:"dirty"`
}

// GoneDelta: the buffer was closed or unloaded on the laptop; the topic
// sends nothing more.
type GoneDelta struct {
	Kind string `json:"kind" const:"gone"`
}

// BufOpenRes is the result of buf.open; Lines is the line count.
type BufOpenRes struct {
	Buf      int    `json:"buf"`
	Path     string `json:"path"`
	Tick     int    `json:"tick"`
	Lines    int    `json:"lines"`
	Dirty    bool   `json:"dirty"`
	Ft       string `json:"ft"`
	Readonly bool   `json:"readonly"`
}

// BufHeadRes is the result of buf.head: the first lines of a file, read
// only, without loading it into the editor.
type BufHeadRes struct {
	Path      string   `json:"path"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
	Size      int64    `json:"size"`
}

// File view limits: bigger files are refused by buf.open (buf.head
// still shows the first MaxHeadLines lines); a NUL byte in the first
// BinarySniff bytes marks a binary file, which is never sent.
const (
	MaxOpenBytes = 4 << 20
	MaxOpenLines = 60000
	MaxHeadLines = 2000
	BinarySniff  = 8 << 10
)

func init() {
	registerTopic("tree", TreeSnap{}, TreeDirDelta{}, TreeRemoveDelta{}, TreeHiddenDelta{})
	registerTopic("bufs", BufsSnap{}, BufsSnap{})
	registerTopic("buf", BufSnap{}, LinesDelta{}, TickDelta{}, DirtyDelta{}, ResetDelta{}, GoneDelta{})
	registerResult(OpTreeList, TreeListRes{})
	registerResult(OpBufOpen, BufOpenRes{})
	registerResult(OpBufHead, BufHeadRes{})
}
