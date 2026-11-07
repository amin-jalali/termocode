package views

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// ErrInvalid wraps every refusal of a def or a spec. The message names
// the rule broken and, for an unknown id, the ids that are allowed, so
// the AI (or the phone) can fix its spec from the text alone.
var ErrInvalid = errors.New("views: invalid")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Field kinds: how a cell is typed, compared, sorted and shown.
const (
	FieldText  = "text"  // Cell.S; contains / starts are case-insensitive
	FieldNum   = "num"   // Cell.N
	FieldTime  = "time"  // Cell.T, Unix ms
	FieldState = "state" // Cell.S, a state word ("ok", "fail", "running"); drawn with a tone
	FieldPath  = "path"  // Cell.S, workspace- or ~-relative
	FieldLink  = "link"  // Cell.S, a short label of a thing (#418)
	FieldBool  = "bool"  // Cell.B
	FieldCost  = "cost"  // Cell.N, US dollars
	FieldMD    = "md"    // Cell.S, short markdown shown as text
)

// Param kinds: how a spec arg is checked.
const (
	ParamWS       = "ws"        // a project id
	ParamString   = "string"    // one line of text
	ParamInt      = "int"       // a whole number
	ParamEnum     = "enum"      // one of Param.Enum
	ParamPath     = "path"      // a relative path, no ".."
	ParamTable    = "table"     // a table name
	ParamConn     = "conn"      // a connection name
	ParamNumberPR = "number_pr" // a pull request number, 1 or more
	ParamURI      = "uri"       // an MCP resource uri from the server's own list
)

// Densities of a view block.
const (
	DensityCompact  = "compact"
	DensityDetailed = "detailed"
)

// Caps of a def and of what a fetch may give.
const (
	MaxDefFields   = 64   // fields of one view
	MaxDefParams   = 12   // params of one view
	MaxTableFields = 12   // fields a table layout shows (blocks.MaxColumns)
	MaxFetchRows   = 1000 // rows a fetch may give; the spec is applied to these
	DefaultLimit   = 50   // rows shown when neither the spec nor the def says
	MaxEnum        = 32   // values of an enum param
)

var (
	fieldKinds = setOf(FieldText, FieldNum, FieldTime, FieldState, FieldPath, FieldLink, FieldBool, FieldCost, FieldMD)
	paramKinds = setOf(ParamWS, ParamString, ParamInt, ParamEnum, ParamPath, ParamTable, ParamConn, ParamNumberPR, ParamURI)
	// The layouts this package renders from rows. A view may allow other
	// block kinds (diff, text, board); their Fetch builds the block itself.
	rowLayouts = setOf(blocks.KindTable, blocks.KindList, blocks.KindCard, blocks.KindDetail)
	idRe       = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_\-]+)*$`)
	fieldIDRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

func setOf(vs ...string) map[string]bool {
	m := make(map[string]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

// Source says where a view's data comes from on the laptop. Exactly one
// of Topic, Op, Tool, MCPTool or MCPResource is set.
type Source struct {
	Topic       string // a webproto topic ("git", "runs")
	Op          string // a webproto op ("pr.list")
	Tool        string // a read-only AI tool ("search")
	MCPServer   string // with MCPTool or MCPResource
	MCPTool     string
	MCPResource bool
}

func (s Source) count() int {
	n := 0
	for _, b := range []bool{s.Topic != "", s.Op != "", s.Tool != "", s.MCPTool != "", s.MCPResource} {
		if b {
			n++
		}
	}
	return n
}

// Param is one arg a view takes.
type Param struct {
	ID       string
	Label    string
	Kind     string
	Enum     []string // ParamEnum only
	Required bool
	Default  string
}

// Field is one typed column of a view's rows.
type Field struct {
	ID         string
	Label      string
	Kind       string
	Sortable   bool
	Filterable bool
	Default    bool // shown when the spec names no fields
}

// Def is one view: a source, params, typed fields, the layouts it allows
// and its default spec.
type Def struct {
	ID      string // "git.status", "pr.list", "mcp.github.list_issues"
	Feature string // "git", "ci", "mcp", …
	Title   string
	Source  Source
	Params  []Param
	Fields  []Field
	Layouts []string // allowed block kinds; the first is the default
	Default blocks.ViewSpec
	Actions []blocks.BlockAction // row actions; their Ref comes from the row
	Role    string               // least role ("viewer", "controller"); never lower than the source op's
	Live    bool                 // topic-backed: the hub re-runs the spec on each change
	Refresh int                  // op-backed: default seconds between re-runs when pinned (0: on tap)
}

// Field returns the field with id.
func (d *Def) Field(id string) (Field, bool) {
	for _, f := range d.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return Field{}, false
}

// Param returns the param with id.
func (d *Def) Param(id string) (Param, bool) {
	for _, p := range d.Params {
		if p.ID == id {
			return p, true
		}
	}
	return Param{}, false
}

// Check reports whether the def itself is well formed: ids, kinds,
// layouts, one source, and a default spec that passes CheckSpec.
func (d *Def) Check() error {
	if !idRe.MatchString(d.ID) || len(d.ID) > blocks.MaxID {
		return invalid("view id %q", d.ID)
	}
	if d.Feature == "" || d.Title == "" {
		return invalid("view %s: no feature or title", d.ID)
	}
	if d.Source.count() != 1 {
		return invalid("view %s: needs exactly one source", d.ID)
	}
	if (d.Source.MCPTool != "" || d.Source.MCPResource) == (d.Source.MCPServer == "") {
		return invalid("view %s: an MCP source names its server, others do not", d.ID)
	}
	if len(d.Fields) == 0 || len(d.Fields) > MaxDefFields {
		return invalid("view %s: %d fields (1 to %d)", d.ID, len(d.Fields), MaxDefFields)
	}
	seen := map[string]bool{}
	for _, f := range d.Fields {
		if !fieldIDRe.MatchString(f.ID) || seen[f.ID] {
			return invalid("view %s: bad or repeated field id %q", d.ID, f.ID)
		}
		seen[f.ID] = true
		if !fieldKinds[f.Kind] {
			return invalid("view %s: field %s: unknown kind %q", d.ID, f.ID, f.Kind)
		}
		if f.Label == "" {
			return invalid("view %s: field %s: no label", d.ID, f.ID)
		}
	}
	if len(d.Params) > MaxDefParams {
		return invalid("view %s: %d params, max %d", d.ID, len(d.Params), MaxDefParams)
	}
	seen = map[string]bool{}
	for _, p := range d.Params {
		if !fieldIDRe.MatchString(p.ID) || seen[p.ID] {
			return invalid("view %s: bad or repeated param id %q", d.ID, p.ID)
		}
		seen[p.ID] = true
		if !paramKinds[p.Kind] {
			return invalid("view %s: param %s: unknown kind %q", d.ID, p.ID, p.Kind)
		}
		if (p.Kind == ParamEnum) != (len(p.Enum) > 0) || len(p.Enum) > MaxEnum {
			return invalid("view %s: param %s: enum values go with the enum kind only (1 to %d)", d.ID, p.ID, MaxEnum)
		}
		if p.Default != "" {
			if err := checkArg(p, p.Default); err != nil {
				return invalid("view %s: param %s: bad default: %v", d.ID, p.ID, err)
			}
		}
	}
	if len(d.Layouts) == 0 {
		return invalid("view %s: no layouts", d.ID)
	}
	seen = map[string]bool{}
	for _, l := range d.Layouts {
		if seen[l] || !kindOK(l) {
			return invalid("view %s: bad or repeated layout %q", d.ID, l)
		}
		seen[l] = true
	}
	if d.Live && d.Source.Topic == "" {
		return invalid("view %s: only a topic-backed view is live", d.ID)
	}
	if d.Refresh != 0 && (d.Live || d.Refresh < blocks.MinSpecRefresh) {
		return invalid("view %s: refresh is for op-backed views, at least %d s", d.ID, blocks.MinSpecRefresh)
	}
	def := d.Default
	def.View = d.ID
	if err := checkSpec(d, &def, false); err != nil {
		return fmt.Errorf("view %s: default spec: %w", d.ID, err)
	}
	return nil
}

func kindOK(k string) bool {
	for _, x := range blocks.Kinds() {
		if x == k {
			return true
		}
	}
	return false
}

// Cell is one typed value of a row. Which member counts follows the
// field's kind; Null marks "no value" (sorted last, matches empty).
type Cell struct {
	S    string
	N    float64
	T    int64 // Unix ms
	B    bool
	Null bool
}

// Row is one row a source gave: typed cells by field id, plus what the
// phone needs to act on it.
type Row struct {
	ID      string
	Cells   map[string]Cell
	Tone    string           // overrides the tone a state field gives
	Ref     *blocks.BlockRef // what "↩ to chat" and row actions send
	Actions []blocks.BlockAction
}
