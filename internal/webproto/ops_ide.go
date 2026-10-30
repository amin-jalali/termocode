package webproto

import "strings"

// The ops of the desktop workbench's explorer sections: the OUTLINE (the
// symbols of the open file) and the TIMELINE (the commits of one file). They join the op table below; every rule of
// ops.go holds for them.

// Explorer op names.
const (
	OpGitFileLog = "git.file_log"
	OpBufSymbols = "buf.symbols"
	OpGitText    = "git.text"
)

// BufSymbols is the result of buf.symbols: the symbols of a buffer the
// page opened, as a flat list in file order (Depth nests them). Source
// says who found them: the language server ("lsp"), the hub's own lexer
// ("syntax", a guess from declarations) or nobody ("none").
type BufSymbols struct {
	Buf     int         `json:"buf"`
	Source  string      `json:"source" enum:"lsp|syntax|none"`
	Symbols []BufSymbol `json:"symbols"`
	Cut     bool        `json:"cut,omitempty"` // more than MaxSymbols: the rest is left out
}

// BufSymbol is one symbol: its name, the language server's kind (LSP
// SymbolKind: 5 class, 6 method, 12 function, …), where its name starts
// (1-based line, 1-based UTF-16 column) and how deep it nests.
type BufSymbol struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Kind   int    `json:"kind"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
	Depth  int    `json:"depth"`
}

// MaxSymbols caps the symbols of one buf.symbols.
const MaxSymbols = 2000

// GitFileLogArgs names the file whose commits the timeline lists.
type GitFileLogArgs struct {
	Path string `json:"path"`
}

// GitFileLog is the result of git.file_log: the last commits that
// changed the file (following renames), newest first.
type GitFileLog struct {
	Path string      `json:"path"`
	Log  []GitCommit `json:"log"`
}

// GitTextArgs asks for one file as git keeps it: Rev is "index", "HEAD",
// a commit hash, or a hash followed by "^" (its parent). The desktop's
// diff editor shows it beside the newer text.
type GitTextArgs struct {
	Path string `json:"path"`
	Rev  string `json:"rev"`
}

// GitText is the result of git.text.
type GitText struct {
	Path      string `json:"path"`
	Rev       string `json:"rev"`
	Text      string `json:"text"`
	Missing   bool   `json:"missing,omitempty"`   // the file is not in that revision (a new file)
	Binary    bool   `json:"binary,omitempty"`    // not text: Text is empty
	Truncated bool   `json:"truncated,omitempty"` // longer than MaxGitText: Text is its start
}

// MaxGitText caps the bytes of one git.text.
const MaxGitText = 512 << 10

// MaxFileLog caps the commits of one git.file_log.
const MaxFileLog = 50

var opIDERows = []Policy{
	{Op: OpGitFileLog, New: newOf[GitFileLogArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpGitText, New: newOf[GitTextArgs](), Role: RoleViewer, Rate: perSec(10), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpBufSymbols, New: newOf[BufArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

func (a *GitFileLogArgs) validate(int) error {
	if !validPath(a.Path, false) {
		return invalid("bad path")
	}
	return nil
}

func (a *GitTextArgs) validate(int) error {
	if !validPath(a.Path, false) || strings.HasSuffix(a.Path, "/") {
		return invalid("bad path")
	}
	if a.Rev == "index" || a.Rev == "HEAD" {
		return nil
	}
	h := strings.TrimSuffix(a.Rev, "^")
	if len(h) < 4 || len(h) > 64 {
		return invalid("bad revision")
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return invalid("bad revision")
		}
	}
	return nil
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opIDERows...); return true }()

func init() {
	registerResult(OpGitFileLog, GitFileLog{})
	registerResult(OpGitText, GitText{})
	registerResult(OpBufSymbols, BufSymbols{})
}
