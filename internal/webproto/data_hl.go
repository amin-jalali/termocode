package webproto

// Syntax colours of the file view (WEB-38, web-ui-design §2.4): nvim's own
// tree-sitter captures for the rows on screen, so the browser never parses
// code. A span is [row, c0, c1, g]: buffer row, start and end column in
// UTF-16 code units of that row (what the page's strings count) and an
// index into Groups (capture names such as "keyword" or "string.escape").

// HlArgs asks the colours of rows [First, Last) of buffer Buf.
type HlArgs struct {
	Buf   int `json:"buf"`
	First int `json:"first"`
	Last  int `json:"last"`
}

// HlRes is the result of buf.hl. Tick is the changedtick the spans belong
// to (the page drops spans of a tick it no longer shows). None: the
// buffer has no parser (plain text). Cut: MaxHlSpans was reached.
type HlRes struct {
	Tick   int      `json:"tick"`
	First  int      `json:"first"`
	Last   int      `json:"last"`
	Groups []string `json:"groups"`
	Spans  [][]int  `json:"spans"`
	None   bool     `json:"none,omitempty"`
	Cut    bool     `json:"cut,omitempty"`
}

// Limits of buf.hl: rows per request and spans per answer.
const (
	MaxHlRows  = 2000
	MaxHlSpans = 40000
)

func (a *HlArgs) validate(int) error {
	if !validBuf(a.Buf) || a.First < 0 || a.Last < a.First || a.Last-a.First > MaxHlRows {
		return invalid("bad buffer or rows")
	}
	return nil
}

func init() {
	registerResult(OpBufHl, HlRes{})
}
