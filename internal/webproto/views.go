package webproto

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// The view registry (mobile plan 3.11, MOB-9): one typed view per
// feature. A view says where its rows come from (a topic, a read op, an
// AI tool or an MCP tool), which params it takes, which typed fields its
// rows have, which layouts it may show as and its default spec. The page
// lists the registry (view.catalog), builds a spec (the Views menu, the
// Customize sheet, the AI's view_show) and runs it (view.run); the laptop
// alone applies a spec (filter, sort, group, limit, layout: ApplyView),
// so there is one implementation and one set of fixtures.
//
// A view never adds a write path: its row actions name existing ops, and
// its role is never lower than its source op's (policy walk test).

// View field kinds: how a cell is typed, filtered, sorted and shown.
const (
	FieldText  = "text"
	FieldNum   = "num"
	FieldTime  = "time" // Unix ms
	FieldState = "state"
	FieldPath  = "path" // workspace- or ~-relative
	FieldLink  = "link"
	FieldBool  = "bool"
	FieldCost  = "cost" // USD
	FieldMD    = "md"
)

// View param kinds.
const (
	ParamWS       = "ws"
	ParamString   = "string"
	ParamInt      = "int"
	ParamEnum     = "enum"
	ParamPath     = "path"
	ParamTable    = "table"
	ParamConn     = "conn"
	ParamNumberPR = "number_pr"
	ParamURI      = "uri"
)

// View features, in the menu's order (mobile plan 2.2).
var ViewFeatures = []string{"git", "lanes", "runs", "workflows", "tasks", "projects", "inbox", "chat",
	"ci", "db", "http", "containers", "kube", "tests", "services", "search", "logs", "usage",
	"diagnostics", "debug", "settings", "mcp", "lsp", "ext", "terms", "files"}

// View limits.
const (
	MaxLiveViews     = 8  // live views per connection
	DefaultViewLimit = 50 // rows when a spec says none
	GHViewRefresh    = 60 // seconds at least between re-runs of a gh-backed view
	MaxViewRows      = 1000
)

// ViewSource says where a view's rows come from: exactly one of Topic,
// Op, Tool or MCPServer (+ MCPTool, or none for the server's resources).
type ViewSource struct {
	Topic     string `json:"topic,omitempty"`
	Op        string `json:"op,omitempty"`
	Tool      string `json:"tool,omitempty"`
	MCPServer string `json:"mcp_server,omitempty"`
	MCPTool   string `json:"mcp_tool,omitempty"`
}

// ViewParam is one param of a view (the params form on the page).
type ViewParam struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind" enum:"ws|string|int|enum|path|table|conn|number_pr|uri"`
	Enum     []string `json:"enum,omitempty"`
	Required bool     `json:"required,omitempty"`
}

// ViewField is one typed field of a view's rows.
type ViewField struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Kind       string `json:"kind" enum:"text|num|time|state|path|link|bool|cost|md"`
	Sortable   bool   `json:"sortable,omitempty"`
	Filterable bool   `json:"filterable,omitempty"`
	Default    bool   `json:"default,omitempty"` // shown when a spec names no fields
}

// ViewDef is one view of the registry.
type ViewDef struct {
	ID      string        `json:"id"`
	Feature string        `json:"feature"`
	Title   string        `json:"title"`
	Source  ViewSource    `json:"source"`
	Params  []ViewParam   `json:"params,omitempty"`
	Fields  []ViewField   `json:"fields"`
	Layouts []string      `json:"layouts"` // block kinds; the first is the default
	Default ViewSpec      `json:"default"`
	Actions []BlockAction `json:"actions,omitempty"` // row actions; their args come from the row's Ref
	Role    Role          `json:"role" enum:"viewer|controller"`
	Live    bool          `json:"live,omitempty"`    // topic-backed: the hub pushes updates
	Refresh int           `json:"refresh,omitempty"` // op-backed: default seconds between re-runs when pinned
}

// Field returns the field with id.
func (d ViewDef) Field(id string) (ViewField, bool) {
	for _, f := range d.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return ViewField{}, false
}

// GH says the view reads through the laptop's gh (re-runs ≥ 60 s apart).
func (d ViewDef) GH() bool {
	return strings.HasPrefix(d.Source.Op, "pr.") || strings.HasPrefix(d.Source.Op, "ci.")
}

var viewTable []ViewDef

// registerViews adds views to the registry (init only). A view's role is
// raised to its source op's; a duplicate id, an unknown feature or topic,
// a source that is not exactly one, or a default spec that does not pass
// CheckViewSpec is a programming error.
func registerViews(defs ...ViewDef) {
	for _, d := range defs {
		if _, dup := LookupView(d.ID); dup {
			panic("webproto: view registered twice: " + d.ID)
		}
		if !slices.Contains(ViewFeatures, d.Feature) {
			panic("webproto: view " + d.ID + ": unknown feature " + d.Feature)
		}
		n := 0
		for _, s := range []string{d.Source.Topic, d.Source.Op, d.Source.Tool, d.Source.MCPServer} {
			if s != "" {
				n++
			}
		}
		if n != 1 {
			panic("webproto: view " + d.ID + ": a view has exactly one source")
		}
		if d.Source.Topic != "" {
			if _, ok := topicDef(d.Source.Topic); !ok {
				panic("webproto: view " + d.ID + ": unknown topic " + d.Source.Topic)
			}
			d.Live = true
		}
		if d.Role == "" {
			d.Role = RoleViewer
		}
		if d.Source.Op != "" {
			p, ok := Lookup(d.Source.Op)
			if !ok {
				panic("webproto: view " + d.ID + ": unknown op " + d.Source.Op)
			}
			if p.Role == RoleController {
				d.Role = RoleController
			}
		}
		if len(d.Layouts) == 0 {
			panic("webproto: view " + d.ID + ": no layout")
		}
		d.Default.View = d.ID
		if err := checkViewSpec(&d.Default, d, false); err != nil {
			panic("webproto: view " + d.ID + ": default spec: " + err.Error())
		}
		viewTable = append(viewTable, d)
	}
}

// Views returns the registry in feature order, then registration order.
func Views() []ViewDef {
	out := append([]ViewDef(nil), viewTable...)
	slices.SortStableFunc(out, func(a, b ViewDef) int {
		return slices.Index(ViewFeatures, a.Feature) - slices.Index(ViewFeatures, b.Feature)
	})
	return out
}

// LookupView returns the view with id.
func LookupView(id string) (ViewDef, bool) {
	for _, d := range viewTable {
		if d.ID == id {
			return d, true
		}
	}
	return ViewDef{}, false
}

// ViewsFor lists the views a role may run (a view a role may not run is
// hidden from its menu).
func ViewsFor(role Role) []ViewDef {
	var out []ViewDef
	for _, d := range Views() {
		if d.Role == RoleViewer || role == RoleController {
			out = append(out, d)
		}
	}
	return out
}

func viewIDs[T any](xs []T, id func(T) string) string {
	ids := make([]string, len(xs))
	for i, x := range xs {
		ids[i] = id(x)
	}
	return strings.Join(ids, ", ")
}

// CheckViewSpec checks a spec against its view: its shape (sizes, ids,
// no path or URL in an arg), then every param, field, filter, sort key,
// group and layout by id and kind. The message of a refusal names the
// allowed ids, so the AI (or a hand-made link) can fix its spec.
func CheckViewSpec(s *ViewSpec, d ViewDef) error { return checkViewSpec(s, d, true) }

// checkViewSpec is CheckViewSpec; needArgs false skips the required
// params (a view's default spec leaves them to the user).
func checkViewSpec(s *ViewSpec, d ViewDef, needArgs bool) error {
	if err := s.Validate(); err != nil {
		return invalid("%s", strings.TrimPrefix(err.Error(), blocks.ErrInvalid.Error()+": "))
	}
	if s.View != d.ID {
		return invalid("spec for %q run as %q", s.View, d.ID)
	}
	fieldIDs := viewIDs(d.Fields, func(f ViewField) string { return f.ID })
	for k, v := range s.Args {
		i := slices.IndexFunc(d.Params, func(p ViewParam) bool { return p.ID == k })
		if i < 0 {
			if len(d.Params) == 0 {
				return invalid("view %s takes no params", d.ID)
			}
			return invalid("unknown param %q (one of %s)", k, viewIDs(d.Params, func(p ViewParam) string { return p.ID }))
		}
		if err := checkViewParam(d.Params[i], v); err != nil {
			return err
		}
	}
	for _, p := range d.Params {
		if needArgs && p.Required && s.Args[p.ID] == "" && d.Default.Args[p.ID] == "" {
			return invalid("param %s is needed", p.ID)
		}
	}
	for _, f := range s.Fields {
		if _, ok := d.Field(f); !ok {
			return invalid("unknown field %q (one of %s)", f, fieldIDs)
		}
	}
	for _, c := range s.Filter {
		f, ok := d.Field(c.Field)
		if !ok || !f.Filterable {
			return invalid("cannot filter on %q (filterable: %s)", c.Field, filterableIDs(d))
		}
		switch c.Op {
		case "gt", "lt", "ge", "le":
			if f.Kind != FieldNum && f.Kind != FieldTime && f.Kind != FieldCost {
				return invalid("filter %s: %s needs a num, time or cost field", c.Field, c.Op)
			}
			if _, err := strconv.ParseFloat(c.Value, 64); err != nil {
				return invalid("filter %s: %s needs a number", c.Field, c.Op)
			}
		case "contains", "starts":
			if !textKind(f.Kind) {
				return invalid("filter %s: %s needs a text field", c.Field, c.Op)
			}
		}
	}
	for _, o := range s.Sort {
		f, ok := d.Field(o.Field)
		if !ok || !f.Sortable {
			return invalid("cannot sort on %q (sortable: %s)", o.Field, sortableIDs(d))
		}
	}
	if s.Group != "" {
		if _, ok := d.Field(s.Group); !ok {
			return invalid("cannot group on %q (one of %s)", s.Group, fieldIDs)
		}
	}
	if s.Layout != "" && !slices.Contains(d.Layouts, s.Layout) {
		return invalid("layout %q (one of %s)", s.Layout, strings.Join(d.Layouts, ", "))
	}
	if s.Refresh != 0 {
		if d.Source.Topic != "" {
			return invalid("view %s is live: it takes no refresh", d.ID)
		}
		if d.GH() && s.Refresh < GHViewRefresh {
			return invalid("refresh %d s: this view reads GitHub, at least %d s", s.Refresh, GHViewRefresh)
		}
	}
	return nil
}

func textKind(k string) bool {
	return k == FieldText || k == FieldState || k == FieldPath || k == FieldLink || k == FieldMD
}

func filterableIDs(d ViewDef) string {
	var ids []string
	for _, f := range d.Fields {
		if f.Filterable {
			ids = append(ids, f.ID)
		}
	}
	return strings.Join(ids, ", ")
}

func sortableIDs(d ViewDef) string {
	var ids []string
	for _, f := range d.Fields {
		if f.Sortable {
			ids = append(ids, f.ID)
		}
	}
	return strings.Join(ids, ", ")
}

func checkViewParam(p ViewParam, v string) error {
	switch p.Kind {
	case ParamInt, ParamNumberPR:
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || (p.Kind == ParamNumberPR && (n < 1 || n > MaxPRNumber)) {
			return invalid("param %s: a whole number", p.ID)
		}
	case ParamEnum:
		if !slices.Contains(p.Enum, v) {
			return invalid("param %s: one of %s", p.ID, strings.Join(p.Enum, ", "))
		}
	case ParamWS, ParamConn, ParamTable:
		if v != "" && !viewIDRe(v) {
			return invalid("param %s: a name, not %q", p.ID, v)
		}
	case ParamPath:
		if strings.Contains(v, "..") {
			return invalid("param %s: a path inside the workspace", p.ID)
		}
	}
	return nil
}

func viewIDRe(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if !(r == '-' || r == '_' || r == '.' || r == ':' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// Effective is the spec with the view's defaults filled in: fields,
// sort, limit, layout, density and the default args.
func Effective(s ViewSpec, d ViewDef) ViewSpec {
	if len(s.Fields) == 0 {
		s.Fields = d.Default.Fields
		if len(s.Fields) == 0 {
			for _, f := range d.Fields {
				if f.Default {
					s.Fields = append(s.Fields, f.ID)
				}
			}
		}
	}
	if len(s.Sort) == 0 {
		s.Sort = d.Default.Sort
	}
	if s.Limit == 0 {
		s.Limit = d.Default.Limit
		if s.Limit == 0 {
			s.Limit = DefaultViewLimit
		}
	}
	if s.Layout == "" {
		s.Layout = d.Default.Layout
		if s.Layout == "" {
			s.Layout = d.Layouts[0]
		}
	}
	if s.Density == "" {
		s.Density = d.Default.Density
	}
	if s.Group == "" {
		s.Group = d.Default.Group
	}
	for k, v := range d.Default.Args {
		if s.Args[k] == "" {
			if s.Args == nil {
				s.Args = map[string]string{}
			}
			s.Args[k] = v
		}
	}
	if s.Refresh == 0 && d.Source.Topic == "" {
		s.Refresh = d.Refresh
	}
	return s
}

// The view ops (MOB-9).
const (
	OpViewRun     = "view.run"
	OpViewStop    = "view.stop"
	OpViewCatalog = "view.catalog"
	// EventViewUpdate: {id, block}: a live view's new block.
	EventViewUpdate = "view_update"
)

// ViewRunArgs runs one spec; Live on a topic-backed view keeps it running
// (the hub sends ev view_update on each change, coalesced) until
// view.stop or the connection ends.
type ViewRunArgs struct {
	Spec ViewSpec `json:"spec"`
	Live bool     `json:"live,omitempty"`
}

func (a *ViewRunArgs) validate(int) error {
	if err := a.Spec.Validate(); err != nil {
		return invalid("%s", err.Error())
	}
	return nil
}

// ViewRunResult is the block; Live is its live id when it keeps running
// (the block's id too).
type ViewRunResult struct {
	Block Block  `json:"block"`
	Live  string `json:"live,omitempty"`
}

// ViewStopArgs ends a live view.
type ViewStopArgs struct {
	ID string `json:"id"`
}

func (a *ViewStopArgs) validate(int) error {
	if !strings.HasPrefix(a.ID, "lv-") || len(a.ID) > 32 || !viewIDRe(a.ID) {
		return invalid("bad live view id")
	}
	return nil
}

// ViewCatalog is the registry a connection may run, in menu order.
type ViewCatalog struct {
	Features []string  `json:"features"`
	Views    []ViewDef `json:"views"`
}

// ViewUpdate is the data of ev view_update.
type ViewUpdate struct {
	ID    string `json:"id"`
	Block Block  `json:"block"`
}

var opViewRows = []Policy{
	{Op: OpViewRun, New: newOf[ViewRunArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 8 * kib, Live: 2, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpViewStop, New: newOf[ViewStopArgs](), Role: RoleViewer, Rate: perSec(10), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpViewCatalog, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opViewRows...); return true }()

func init() {
	registerResult(OpViewRun, ViewRunResult{})
	registerResult(OpViewCatalog, ViewCatalog{})
	registerEvent(EventViewUpdate, ViewUpdate{})
}
