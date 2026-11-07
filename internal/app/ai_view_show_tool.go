package app

// MOBUX-17: the AI builds a view from a chat command ("orders table,
// only status and total, today"). The model calls view_show with a view
// spec; the spec is checked and resolved on the laptop against the view
// registry (internal/views), run on the laptop (the hub's in-process
// view.run, MOB-9), and the block goes to the tool line through the
// tool's View hook (MOB-10). The phone draws it with a spec chip that
// says what was built and opens the customizer (MOBUX-16). The model
// gets only a short text: the title, the total, what was built and the
// first rows — never more than the phone sees (mobile plan 3.11.5).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/views"
)

// aiViewRunner is the view registry and the in-process view.run of the
// web hub (MOB-9). RunView gets a spec that views.Resolve already
// checked and filled; its block holds redacted rows only.
type aiViewRunner interface {
	ViewDef(id string) (*views.Def, bool)
	ViewIDs() []string
	// CanShow says whether the view's source only reads: a topic, an op,
	// a read-only AI tool or a read-only, enabled, in-profile MCP tool.
	CanShow(d *views.Def) bool
	RunView(ctx context.Context, spec blocks.ViewSpec) (blocks.Block, error)
}

const (
	aiViewShowRows     = 20 // rows the model gets as text
	aiViewShowCellCut  = 80 // runes of one cell in that text
	aiViewShowMaxIDs   = 40 // view ids named in an "unknown view" error
	aiViewShowSpecSize = 8 << 10
)

var errViewShowNoViews = errors.New("views are not ready on this laptop (the phone page is off)")

// aiViewShown is the block a view_show call built, kept for the tool's
// View hook (keyed by the call's args map, like the HTTP tools).
type aiViewShown struct {
	block blocks.Block
	// args keeps the map alive while its entry is cached, so its address
	// can never be given to a new map that would then get this block.
	args map[string]any
}

var aiViewShowCache sync.Map // args map pointer → *aiViewShown

func aiViewShowKey(a map[string]any) uintptr { return reflect.ValueOf(a).Pointer() }

func aiViewShowForget(a map[string]any) {
	if a != nil {
		aiViewShowCache.Delete(aiViewShowKey(a))
	}
}

// aiViewShowTools is view_show over r (none while r is nil: the web
// side is off, so there is no phone to show a view on).
func aiViewShowTools(r aiViewRunner) []ai.Tool {
	if r == nil {
		return nil
	}
	return []ai.Tool{{
		Name: "view_show",
		Description: "Show the user a live view on their phone, built from a view spec: " +
			`{"view": id, "args": {param: value}, "fields": [field ids, in order], ` +
			`"filter": [{"field", "op": eq|ne|gt|lt|ge|le|contains|starts|in|empty|not_empty, "value" or "in": [..]}], ` +
			`"sort": [{"field", "desc"}], "group": field, "limit": 1..200, "layout": table|list|card|detail, ` +
			`"density": compact|detailed, "title": short label}. ` +
			"Time values take today, yesterday, 7d, 24h or a date. Use it when the user asks to see, list or show data " +
			`("orders table, only status and total, today"). A wrong id is answered with the allowed ids; fix the spec and call again. ` +
			"Returns the title, the total, what was built and the first 20 rows as text. Specs never carry SQL, shell lines, absolute paths or URLs.",
		Params: map[string]any{
			"spec": map[string]any{"type": "object", "description": "the view spec"},
		},
		Required: []string{"spec"},
		ReadOnly: true,
		Validate: func(a map[string]any) error {
			_, _, err := aiViewShowSpec(r, a)
			return err
		},
		Describe: func(a map[string]any) string {
			d, s, err := aiViewShowSpec(r, a)
			if err != nil {
				return "Show a view"
			}
			return "Show view " + aiViewTitle(d, s)
		},
		Release: aiViewShowForget,
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			return aiViewShowRun(ctx, r, a)
		},
		View: func(a map[string]any, _ string) *blocks.Block {
			v, ok := aiViewShowCache.LoadAndDelete(aiViewShowKey(a))
			if !ok {
				return nil
			}
			b := v.(*aiViewShown).block
			return &b
		},
	}}
}

// aiViewShowSpec reads, checks and resolves the spec of a call: the
// shape (blocks), then every id against the view's def (views).
func aiViewShowSpec(r aiViewRunner, a map[string]any) (*views.Def, blocks.ViewSpec, error) {
	var s blocks.ViewSpec
	raw, err := aiViewSpecJSON(a["spec"])
	if err != nil {
		return nil, s, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, s, fmt.Errorf("spec: %v", err)
	}
	if err := s.Validate(); err != nil {
		return nil, s, err
	}
	d, ok := r.ViewDef(s.View)
	if !ok {
		return nil, s, fmt.Errorf("unknown view %q; views: %s", s.View, aiViewIDList(r.ViewIDs()))
	}
	if !r.CanShow(d) {
		return nil, s, fmt.Errorf("view %s runs a tool that changes things: use the tool itself", d.ID)
	}
	res, err := views.Resolve(d, s)
	if err != nil {
		return nil, s, err
	}
	return d, res, nil
}

// aiViewSpecJSON is the spec arg as JSON: an object, or a JSON string of
// one (some models send it quoted).
func aiViewSpecJSON(v any) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return nil, errors.New("spec is required")
	case string:
		t = strings.TrimSpace(t)
		if !strings.HasPrefix(t, "{") {
			return nil, errors.New("spec must be an object")
		}
		if len(t) > aiViewShowSpecSize {
			return nil, errors.New("spec is too long")
		}
		return []byte(t), nil
	case map[string]any:
		raw, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("spec: %v", err)
		}
		if len(raw) > aiViewShowSpecSize {
			return nil, errors.New("spec is too long")
		}
		return raw, nil
	default:
		return nil, errors.New("spec must be an object")
	}
}

func aiViewIDList(ids []string) string {
	if len(ids) == 0 {
		return "(none)"
	}
	more := ""
	if len(ids) > aiViewShowMaxIDs {
		more = fmt.Sprintf(" … %d more", len(ids)-aiViewShowMaxIDs)
		ids = ids[:aiViewShowMaxIDs]
	}
	return strings.Join(ids, ", ") + more
}

func aiViewShowRun(ctx context.Context, r aiViewRunner, a map[string]any) (string, error) {
	d, s, err := aiViewShowSpec(r, a)
	if err != nil {
		return "", err
	}
	b, err := r.RunView(ctx, s)
	if err != nil {
		return "", err
	}
	if b.Spec == nil {
		b.Spec = &s
	}
	if b.Source == "" {
		b.Source = d.ID
	}
	if a != nil {
		aiViewShowCache.Store(aiViewShowKey(a), &aiViewShown{block: b, args: a})
	}
	return aiViewShowText(d, *b.Spec, b), nil
}

// aiViewTitle is the spec's own title, else the def's.
func aiViewTitle(d *views.Def, s blocks.ViewSpec) string {
	if t := strings.TrimSpace(s.Title); t != "" {
		return t
	}
	if d.Title != "" {
		return d.Title
	}
	return d.ID
}

// aiViewFieldLabel is a field's label (its id when it has none).
func aiViewFieldLabel(d *views.Def, id string) string {
	if f, ok := d.Field(id); ok && f.Label != "" {
		return f.Label
	}
	return id
}

var aiViewOpWords = map[string]string{
	"eq": "=", "ne": "≠", "gt": ">", "lt": "<", "ge": "≥", "le": "≤",
	"contains": "has", "starts": "starts with", "in": "in", "empty": "is empty", "not_empty": "is set",
}

// aiViewSpecSummary is the one line that says what a spec built, the
// same words as the phone's spec chip (views/home/specsummary.ts):
// "table · Status, Total · Created ≥ today · Total ↓ · by Status · 50 rows".
func aiViewSpecSummary(d *views.Def, s blocks.ViewSpec) string {
	var parts []string
	if s.Layout != "" {
		parts = append(parts, s.Layout)
	}
	if len(s.Fields) > 0 {
		ls := make([]string, len(s.Fields))
		for i, f := range s.Fields {
			ls[i] = aiViewFieldLabel(d, f)
		}
		parts = append(parts, strings.Join(ls, ", "))
	}
	for _, c := range s.Filter {
		w := aiViewOpWords[c.Op]
		if w == "" {
			w = c.Op
		}
		p := aiViewFieldLabel(d, c.Field) + " " + w
		switch c.Op {
		case "empty", "not_empty":
		case "in":
			p += " " + strings.Join(c.In, "|")
		default:
			p += " " + c.Value
		}
		parts = append(parts, p)
	}
	for _, o := range s.Sort {
		arrow := "↑"
		if o.Desc {
			arrow = "↓"
		}
		parts = append(parts, aiViewFieldLabel(d, o.Field)+" "+arrow)
	}
	if s.Group != "" {
		parts = append(parts, "by "+aiViewFieldLabel(d, s.Group))
	}
	if s.Limit > 0 {
		parts = append(parts, strconv.Itoa(s.Limit)+" rows")
	}
	if s.Density == views.DensityCompact {
		parts = append(parts, "compact")
	}
	return strings.Join(parts, " · ")
}

// aiViewShowText is what the model gets: the title, the total, what was
// built and the block's first rows as text.
func aiViewShowText(d *views.Def, s blocks.ViewSpec, b blocks.Block) string {
	var sb strings.Builder
	rows := aiViewBlockLines(b)
	total := b.Total
	if total < len(rows) {
		total = len(rows)
	}
	fmt.Fprintf(&sb, "Shown on the phone: %q (%s), %d row(s)", aiViewTitle(d, s), d.ID, total)
	if b.Truncated {
		sb.WriteString(", cut")
	}
	sb.WriteString(".\nBuilt: " + aiViewSpecSummary(d, s) + "\n")
	sb.WriteString("The user can change it with ⚙ under your answer.\n")
	if len(rows) == 0 {
		sb.WriteString("No rows match.\n")
		return sb.String()
	}
	if len(b.Columns) > 0 {
		hs := make([]string, len(b.Columns))
		for i, c := range b.Columns {
			hs[i] = c.Label
		}
		sb.WriteString(strings.Join(hs, " | ") + "\n")
	}
	shown := rows
	if len(shown) > aiViewShowRows {
		shown = shown[:aiViewShowRows]
	}
	for _, l := range shown {
		sb.WriteString(l + "\n")
	}
	if n := total - len(shown); n > 0 {
		fmt.Fprintf(&sb, "… %d more on the phone\n", n)
	}
	return sb.String()
}

// aiViewBlockLines is one text line per row, item or field of b.
func aiViewBlockLines(b blocks.Block) []string {
	cell := func(s string) string { return aiViewCut(strings.ReplaceAll(s, "\n", " ")) }
	var out []string
	switch {
	case len(b.Rows) > 0:
		for _, r := range b.Rows {
			cs := make([]string, len(r))
			for i, c := range r {
				cs[i] = cell(c)
			}
			out = append(out, strings.Join(cs, " | "))
		}
	case len(b.Groups) > 0:
		for _, g := range b.Groups {
			out = append(out, "## "+cell(g.Label)+" ("+strconv.Itoa(g.Count)+")")
			for _, r := range g.Rows {
				cs := make([]string, len(r))
				for i, c := range r {
					cs[i] = cell(c)
				}
				out = append(out, strings.Join(cs, " | "))
			}
			for _, it := range g.Items {
				out = append(out, aiViewItemLine(it, cell))
			}
		}
	case len(b.Items) > 0:
		for _, it := range b.Items {
			out = append(out, aiViewItemLine(it, cell))
		}
	case len(b.Fields) > 0:
		for _, f := range b.Fields {
			out = append(out, cell(f.Label)+": "+cell(f.Value))
		}
	}
	return out
}

func aiViewItemLine(it blocks.BlockItem, cell func(string) string) string {
	l := cell(it.Title)
	if it.Sub != "" {
		l += " — " + cell(it.Sub)
	}
	if it.Badge != "" {
		l += " [" + cell(it.Badge) + "]"
	}
	return l
}

func aiViewCut(s string) string {
	rs := []rune(s)
	if len(rs) <= aiViewShowCellCut {
		return s
	}
	return string(rs[:aiViewShowCellCut-1]) + "…"
}
