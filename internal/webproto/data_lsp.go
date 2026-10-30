package webproto

// The language features of the desktop editor (web UI plan: Problems,
// hover, completion, go to definition, find references). Nothing runs in
// the browser: the laptop's nvim asks its own language servers
// (vim.lsp, vim.diagnostic) and the hub sends the answers. Every column
// on the wire counts UTF-16 code units of its row (what the page's
// strings count); rows are zero-based.

// Op names. All four only read: a completion the user takes changes the
// text in the editor, and that change goes to the laptop like any typing
// (buf.set_text, guarded by the changedtick).
const (
	OpLspHover      = "lsp.hover"
	OpLspComplete   = "lsp.complete"
	OpLspDefinition = "lsp.definition"
	OpLspReferences = "lsp.references"
)

var opLspRows = []Policy{
	{Op: OpLspHover, New: newOf[LspPosArgs](), Role: RoleViewer, Rate: perSec(10), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpLspComplete, New: newOf[LspPosArgs](), Role: RoleViewer, Rate: perSec(10), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpLspDefinition, New: newOf[LspPosArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpLspReferences, New: newOf[LspPosArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opLspRows...); return true }()

func init() {
	registerTopic("diagnostics", DiagSnap{}, DiagSnap{})
	registerResult(OpLspHover, LspHover{})
	registerResult(OpLspComplete, LspCompletion{})
	registerResult(OpLspDefinition, LspLocations{})
	registerResult(OpLspReferences, LspLocations{})
}

// Limits of the language features.
const (
	MaxDiagItems    = 2000      // diagnostics in the topic; More counts the rest
	MaxDiagPerFile  = 500       // diagnostics of one file in the topic
	MaxDiagMessage  = 2 << 10   // bytes of one diagnostic's message
	MaxHoverText    = 16 << 10  // bytes of hover markdown read from the servers
	MaxCompletions  = 200       // items of one completion answer
	MaxCompleteText = 1 << 10   // bytes of an item's label, insert text or detail
	MaxCompleteDoc  = 2 << 10   // bytes of an item's documentation
	MaxCompleteEdit = 16        // extra edits of one item (an import line)
	MaxLocations    = 500       // locations of one definition or references answer
	MaxLocationText = 512       // bytes of a location's row text
	MaxLspRow       = 1<<31 - 1 // rows and columns fit an int32
)

// LspPosArgs asks about a position of buffer Buf as the page has it at
// changedtick Tick: Line is zero-based, Col counts UTF-16 units of the
// row. A buffer that moved on since is a conflict (the page asks again
// once its own changes reached the laptop).
type LspPosArgs struct {
	Buf  int `json:"buf"`
	Tick int `json:"tick"`
	Line int `json:"line"`
	Col  int `json:"col"`
}

func (a *LspPosArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick < 0 || a.Line < 0 || a.Col < 0 || a.Line > MaxLspRow || a.Col > MaxLspRow || a.Tick > MaxLspRow {
		return invalid("bad buffer or position")
	}
	return nil
}

// DiagSnap is the diagnostics topic: what the laptop's language servers
// (and task problem matchers) report for the workspace's files, as the
// laptop's Problems panel lists them. Files go worst first, then by path;
// a file's items by severity, then row and column. The counts are of
// every diagnostic in the workspace, also those past the caps. The delta
// is the whole snapshot again.
type DiagSnap struct {
	Files    []DiagFile `json:"files"`
	Errors   int        `json:"errors"`
	Warnings int        `json:"warnings"`
	Infos    int        `json:"infos"`
	Hints    int        `json:"hints"`
	More     int        `json:"more,omitempty"` // diagnostics not listed (MaxDiagItems, MaxDiagPerFile)
}

// DiagFile is one file's diagnostics. Path is workspace-relative.
type DiagFile struct {
	Path  string `json:"path"`
	Items []Diag `json:"items"`
}

// Diag is one diagnostic. Sev: 1 error, 2 warning, 3 info, 4 hint. The
// range is [Line:Col, EndLine:EndCol) in UTF-16 units of each row.
type Diag struct {
	Sev     int    `json:"sev"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndLine int    `json:"endLine"`
	EndCol  int    `json:"endCol"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"` // "gopls", "eslint" …
	Code    string `json:"code,omitempty"`   // the server's code for it
}

// LspHover answers lsp.hover: the servers' text as a markdown AST (the
// page builds no markup from strings). None: nothing to show there.
type LspHover struct {
	MD   *MdNode `json:"md,omitempty"`
	None bool    `json:"none,omitempty"`
}

// LspCompletion answers lsp.complete. Incomplete: typing more asks
// again (the servers did not send every item). Cut: MaxCompletions was
// reached.
type LspCompletion struct {
	Items      []LspItem `json:"items"`
	Incomplete bool      `json:"incomplete,omitempty"`
	Cut        bool      `json:"cut,omitempty"`
}

// LspItem is one completion item. Kind is the protocol's
// CompletionItemKind (1 text … 25 type parameter). Insert is the text to
// insert (Snippet: in snippet syntax, $1 and ${2:name} placeholders).
// From and To are the columns of the request's row the item replaces
// (-1: the word at the cursor). Edits are extra changes of the same
// buffer (an import), each [line, col, endLine, endCol] and its text.
type LspItem struct {
	Label   string    `json:"label"`
	Kind    int       `json:"kind,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	Doc     string    `json:"doc,omitempty"` // plain text
	Insert  string    `json:"insert"`
	Snippet bool      `json:"snippet,omitempty"`
	Sort    string    `json:"sort,omitempty"`
	Filter  string    `json:"filter,omitempty"`
	From    int       `json:"from"`
	To      int       `json:"to"`
	Edits   []LspEdit `json:"edits,omitempty"`
}

// LspEdit is one extra text change of a completion item.
type LspEdit struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndLine int    `json:"endLine"`
	EndCol  int    `json:"endCol"`
	Text    string `json:"text"`
}

// LspLocations answers lsp.definition and lsp.references. Cut:
// MaxLocations was reached.
type LspLocations struct {
	Locs []LspLoc `json:"locs"`
	Cut  bool     `json:"cut,omitempty"`
}

// LspLoc is one place in a file. Path is workspace-relative; a place
// outside the workspace has no path, only the file's Name and no text.
// Text is the row (cut at MaxLocationText bytes), empty for a file that
// may hold secrets.
type LspLoc struct {
	Path    string `json:"path,omitempty"`
	Name    string `json:"name,omitempty"`
	Outside bool   `json:"outside,omitempty"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndLine int    `json:"endLine"`
	EndCol  int    `json:"endCol"`
	Text    string `json:"text,omitempty"`
}
