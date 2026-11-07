// Package blocks holds the typed visual blocks the laptop sends to the
// phone (mobile plan 3.2): lists, tables, diffs, cards, details, charts,
// status lines, forms, raw JSON, markdown text and boards. A block is data
// only. The page draws each kind from its fields; nothing in a block is
// HTML or code, and the laptop redacts before it builds one.
//
// It is a leaf package (no termocode imports) so the view registry, the
// AI tools, the MCP mapper and webproto can all use it; webproto aliases
// the types.
package blocks

// Block kinds: the only ones a page draws. An unknown kind on the page
// shows the title and "Open on laptop".
const (
	KindList   = "list"
	KindTable  = "table"
	KindDiff   = "diff"
	KindCard   = "card"
	KindDetail = "detail"
	KindChart  = "chart"
	KindStatus = "status"
	KindForm   = "form"
	KindJSON   = "json"
	KindText   = "text"
	KindBoard  = "board"
)

// Kinds lists every block kind, in the order of the plan.
func Kinds() []string {
	return []string{KindList, KindTable, KindDiff, KindCard, KindDetail, KindChart, KindStatus, KindForm, KindJSON, KindText, KindBoard}
}

// Caps of one block (mobile plan 3.2).
const (
	MaxBlockBytes  = 64 << 10 // the whole block as JSON
	MaxID          = 64       // bytes of an id
	MaxTitle       = 120      // runes of a title, a label or an item title
	MaxSub         = 200      // runes of a sub line, a value or a table cell
	MaxItems       = 200      // list items (also per group and per board column)
	MaxColumns     = 12       // table columns
	MaxRows        = 200      // table rows
	MaxGroups      = 50       // groups of a grouped list or table
	MaxFields      = 40       // card / detail fields
	MaxBars        = 50       // chart bars
	MaxFormFields  = 20       // form fields
	MaxChoices     = 32       // choices of one form field
	MaxJSON        = 16 << 10 // bytes of a json block's text
	MaxActions     = 6        // actions of a block or an item
	MaxDiffLines   = 2000     // lines of a diff over all its hunks
	MaxDiffHunks   = 200      // hunks of a diff
	MaxBoardCols   = 8        // board columns
	MaxPath        = 512      // bytes of a ref path
	MaxForYou      = 400      // runes of the "for you" line
	MaxSpecArgs    = 12       // args of a view spec
	MaxSpecFields  = 40       // fields of a view spec
	MaxSpecFilter  = 8        // filter conditions of a view spec
	MaxSpecSort    = 3        // sort keys of a view spec
	MaxSpecIn      = 16       // values of an "in" condition
	MaxSpecLimit   = 200      // rows a view spec may ask for
	MinSpecRefresh = 10       // seconds between refreshes of an op-backed view
)

// Tones of an item, a field, a bar or a status. The page always draws a
// tone with a shape too (✓ ok, ● warn, ✗ bad, ◐ busy, ○ idle, i info),
// never with colour alone.
const (
	ToneOK   = "ok"
	ToneWarn = "warn"
	ToneBad  = "bad"
	ToneBusy = "busy"
	ToneIdle = "idle"
	ToneInfo = "info"
)

// Block is one visual block. Kind says which payload fields the page
// reads; the others stay empty.
type Block struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind" enum:"list|table|diff|card|detail|chart|status|form|json|text|board"`
	Title     string           `json:"title"`
	Source    string           `json:"source,omitempty"` // view id, tool name or "mcp.<server>.<tool>"
	WS        string           `json:"ws,omitempty"`
	Spec      *ViewSpec        `json:"spec,omitempty"` // the spec that made it (Customize edits this)
	Live      bool             `json:"live,omitempty"` // updates come as ev view_update
	Items     []BlockItem      `json:"items,omitempty"`
	Columns   []BlockColumn    `json:"columns,omitempty"`
	Rows      [][]string       `json:"rows,omitempty"`
	Groups    []BlockGroup     `json:"groups,omitempty"`
	Diff      *BlockDiff       `json:"diff,omitempty"`
	Fields    []BlockField     `json:"fields,omitempty"`
	Bars      []BlockBar       `json:"bars,omitempty"`
	State     string           `json:"state,omitempty" enum:"ok|warn|bad|busy|idle|info"` // status
	Form      []BlockFormField `json:"form,omitempty"`                                    // never a secret kind
	JSON      string           `json:"json,omitempty"`                                    // redacted on the laptop
	MD        *MdNode          `json:"md,omitempty"`
	Board     *BlockBoard      `json:"board,omitempty"`
	ForYou    string           `json:"for_you,omitempty"` // one line the AI wrote for the user
	Ref       *BlockRef        `json:"ref,omitempty"`
	Actions   []BlockAction    `json:"actions,omitempty"`
	Total     int              `json:"total,omitempty"` // rows or items before the limit
	Truncated bool             `json:"truncated,omitempty"`
	Stale     bool             `json:"stale,omitempty"` // a pinned view's last block while offline
}

// BlockItem is one row of a list, a group or a board column.
type BlockItem struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Sub     string        `json:"sub,omitempty"`
	Tone    string        `json:"tone,omitempty" enum:"ok|warn|bad|busy|idle|info"`
	Badge   string        `json:"badge,omitempty"` // a short mono fact ("CI ✗", "+12 −3")
	Time    int64         `json:"time,omitempty"`  // Unix ms; the page shows "2 h"
	Ref     *BlockRef     `json:"ref,omitempty"`
	Actions []BlockAction `json:"actions,omitempty"`
}

// Column kinds of a table.
const (
	ColText = "text"
	ColNum  = "num"
	ColTime = "time"
	ColCost = "cost"
	ColTone = "tone"
	ColMono = "mono"
)

// BlockColumn is one column of a table.
type BlockColumn struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty" enum:"text|num|time|cost|tone|mono"`
}

// BlockGroup is one group of a grouped list (Items) or table (Rows).
type BlockGroup struct {
	Label string      `json:"label"`
	Count int         `json:"count"`
	Items []BlockItem `json:"items,omitempty"`
	Rows  [][]string  `json:"rows,omitempty"`
}

// BlockDiff is the diff of one file.
type BlockDiff struct {
	Path  string      `json:"path"` // workspace-relative or ~-relative
	Old   string      `json:"old,omitempty"`
	Adds  int         `json:"adds,omitempty"`
	Dels  int         `json:"dels,omitempty"`
	Hunks []BlockHunk `json:"hunks"`
}

// BlockHunk is one hunk of a diff.
type BlockHunk struct {
	Header string          `json:"header"`
	Lines  []BlockDiffLine `json:"lines"`
}

// BlockDiffLine is one line of a hunk.
type BlockDiffLine struct {
	K    string `json:"k" enum:"add|del|ctx"`
	A    int    `json:"a,omitempty"` // old line number
	B    int    `json:"b,omitempty"` // new line number
	Text string `json:"text"`
}

// BlockField is one label and value of a card or a detail.
type BlockField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Kind  string `json:"kind,omitempty" enum:"text|num|time|cost|tone|mono"`
	Tone  string `json:"tone,omitempty" enum:"ok|warn|bad|busy|idle|info"`
}

// BlockBar is one bar of a chart (drawn with CSS, no canvas).
type BlockBar struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Text  string  `json:"text,omitempty"` // the value as shown ("$1.20")
	Tone  string  `json:"tone,omitempty" enum:"ok|warn|bad|busy|idle|info"`
}

// Form field kinds. There is no secret kind: a form never asks for a
// password, a token or a key (those stay on the laptop).
const (
	FormText   = "text"
	FormNum    = "num"
	FormBool   = "bool"
	FormChoice = "choice"
	FormLong   = "long"
)

// BlockFormField is one field of a form block.
type BlockFormField struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind" enum:"text|num|bool|choice|long"`
	Value    string   `json:"value,omitempty"`
	Choices  []string `json:"choices,omitempty"`
	Required bool     `json:"required,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}

// BlockBoard is a board: a few columns of cards.
type BlockBoard struct {
	Columns []BlockBoardColumn `json:"columns"`
}

// BlockBoardColumn is one column of a board.
type BlockBoardColumn struct {
	ID    string      `json:"id"`
	Label string      `json:"label"`
	Tone  string      `json:"tone,omitempty" enum:"ok|warn|bad|busy|idle|info"`
	Items []BlockItem `json:"items,omitempty"`
}

// BlockRef points at the thing a block or an item stands for, so the
// phone can send it back (attach) or act on it. It is a reference, never
// the content.
type BlockRef struct {
	Kind string `json:"kind"`           // task, file, pr, run, …
	ID   string `json:"id,omitempty"`   // the thing's id in its own list
	Path string `json:"path,omitempty"` // workspace-relative or ~-relative, never absolute
	Line int    `json:"line,omitempty"`
}

// Action kinds.
const (
	ActOp     = "op"     // run the table op Op; its args come from the Ref
	ActAttach = "attach" // put the Ref into the composer
	ActView   = "view"   // open the view of Spec
	ActLaptop = "laptop" // show the thing on the laptop
)

// BlockAction is one button of a block or an item.
type BlockAction struct {
	ID    string    `json:"id"`
	Label string    `json:"label"`
	Kind  string    `json:"kind" enum:"op|attach|view|laptop"`
	Op    string    `json:"op,omitempty"`
	Ref   *BlockRef `json:"ref,omitempty"`
	Spec  *ViewSpec `json:"spec,omitempty"`
	Risky bool      `json:"risky,omitempty"` // drawn second, arms after 600 ms
}

// ViewSpec says which view to run, with which args, fields, filter,
// sort, group, limit, layout and density (mobile plan 3.11.2). Validate
// checks its shape only; the view registry checks ids against the view.
type ViewSpec struct {
	View    string            `json:"view"`
	WS      string            `json:"ws,omitempty"`
	Args    map[string]string `json:"args,omitempty"`
	Fields  []string          `json:"fields,omitempty"`
	Filter  []ViewCond        `json:"filter,omitempty"`
	Sort    []ViewSort        `json:"sort,omitempty"`
	Group   string            `json:"group,omitempty"`
	Limit   int               `json:"limit,omitempty"`
	Layout  string            `json:"layout,omitempty"`
	Density string            `json:"density,omitempty" enum:"compact|detailed"`
	Title   string            `json:"title,omitempty"`
	Refresh int               `json:"refresh,omitempty"` // seconds, op-backed views only
}

// ViewCond is one filter condition; all of a spec's conditions must hold.
type ViewCond struct {
	Field string   `json:"field"`
	Op    string   `json:"op" enum:"eq|ne|gt|lt|ge|le|contains|starts|in|empty|not_empty"`
	Value string   `json:"value,omitempty"`
	In    []string `json:"in,omitempty"`
}

// ViewSort is one sort key.
type ViewSort struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc,omitempty"`
}

// MdNode is one node of a markdown AST. Raw HTML never becomes a node;
// a link's Href is http or https only (else the link is its text).
// webproto aliases it (webproto.MdNode) with its kind constants.
type MdNode struct {
	K     string     `json:"k" enum:"doc|p|h|em|strong|code|codeblock|ul|ol|li|blockquote|link|hr|br|table|th|td|text"`
	Text  string     `json:"text,omitempty"`  // text, code, codeblock
	C     []MdNode   `json:"c,omitempty"`     // children
	Level int        `json:"level,omitempty"` // h: 1 to 6
	Lang  string     `json:"lang,omitempty"`  // codeblock: the info word
	Href  string     `json:"href,omitempty"`  // link
	Start int        `json:"start,omitempty"` // ol: the first number, when not 1
	Rows  [][]MdNode `json:"rows,omitempty"`  // table: rows of th / td cells, the header row first
}
