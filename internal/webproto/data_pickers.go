package webproto

// The desktop's pickers (WEBP-9, Editor and files) read the laptop's own
// lists: its bookmarks (nvim's global marks A–Z, the laptop's Bookmarks:
// commands), its recent files, the language servers' workspace symbols
// for a query, and the snippets of a file's type. Every path is
// workspace-relative; a place outside the workspace is left out.

// Op names.
const (
	OpBookmarksToggle     = "bookmarks.toggle"
	OpBookmarksList       = "bookmarks.list"
	OpBookmarksClear      = "bookmarks.clear"
	OpFilesRecent         = "files.recent"
	OpLspWorkspaceSymbols = "lsp.workspace_symbols"
	OpSnippetsList        = "snippets.list"
)

// Limits of the pickers.
const (
	MaxSymbolQuery      = 256  // bytes of a workspace symbol query
	MaxWorkspaceSymbols = 500  // workspace symbols of one answer
	MaxSnippets         = 500  // snippets of one file type
	MaxSnippetBody      = 8192 // bytes of one snippet's body
	MaxRecent           = 50   // recent files of one answer
)

// BufLineArgs names row Line (0-based) of buffer Buf.
type BufLineArgs struct {
	Buf  int `json:"buf"`
	Line int `json:"line"`
}

func (a *BufLineArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Line < 0 || a.Line > MaxLspRow {
		return invalid("bad buffer or row")
	}
	return nil
}

// BookmarkToggle answers bookmarks.toggle: the row has a bookmark now.
type BookmarkToggle struct {
	On   bool   `json:"on"`
	Mark string `json:"mark,omitempty"` // the mark's letter
}

// Bookmark is one of the laptop's bookmarks: its letter, file and row
// (0-based) and the row's text when the file is open (none for a secret
// file).
type Bookmark struct {
	Mark string `json:"mark"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text,omitempty"`
}

// BookmarkList answers bookmarks.list, by letter.
type BookmarkList struct {
	Items []Bookmark `json:"items"`
}

// FilesRecent answers files.recent: the laptop's recent files in the
// workspace that still exist, newest first.
type FilesRecent struct {
	Paths []string `json:"paths"`
}

// SymbolQueryArgs asks the language servers of buffer Buf for the
// workspace symbols that match Query.
type SymbolQueryArgs struct {
	Buf   int    `json:"buf"`
	Query string `json:"query"`
}

func (a *SymbolQueryArgs) validate(int) error {
	if !validBuf(a.Buf) || len(a.Query) > MaxSymbolQuery || hasControl(a.Query) {
		return invalid("bad buffer or query")
	}
	return nil
}

// LspSymbol is one workspace symbol: its name, the protocol's
// SymbolKind, where it is (0-based row and column) and what holds it.
type LspSymbol struct {
	Name      string `json:"name"`
	Kind      int    `json:"kind"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	Container string `json:"container,omitempty"`
}

// LspSymbols answers lsp.workspace_symbols.
type LspSymbols struct {
	Items []LspSymbol `json:"items"`
	Cut   bool        `json:"cut,omitempty"`
}

// Snippet is one snippet: what to type and its body in the snippet
// syntax of the language server protocol ($1, ${2:name}, $0).
type Snippet struct {
	Trigger string `json:"trigger"`
	Body    string `json:"body"`
}

// SnippetList answers snippets.list, by trigger.
type SnippetList struct {
	Items []Snippet `json:"items"`
}

var opPickerRows = []Policy{
	{Op: OpBookmarksToggle, New: newOf[BufLineArgs](), Role: RoleController, Rate: perSec(10), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpBookmarksList, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpBookmarksClear, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpFilesRecent, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpLspWorkspaceSymbols, New: newOf[SymbolQueryArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 2 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpSnippetsList, New: newOf[BufArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opPickerRows...); return true }()

func init() {
	registerResult(OpBookmarksToggle, BookmarkToggle{})
	registerResult(OpBookmarksList, BookmarkList{})
	registerResult(OpFilesRecent, FilesRecent{})
	registerResult(OpLspWorkspaceSymbols, LspSymbols{})
	registerResult(OpSnippetsList, SnippetList{})
}
