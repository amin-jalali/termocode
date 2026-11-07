package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// AI view tools (MOB-70, mobile plan 3.11.5). The model reads the view
// registry (view_catalog), builds a view spec and shows it (view_show):
// the laptop checks the spec against the registry, runs it in-process
// and puts the block on the tool line (the View hook, MOB-10), so the
// phone draws it; the model gets a short text of the same rows. The AI
// never sees more than the phone would. view_save keeps a spec on the
// laptop (views.json, MOB-71) and always asks first.

const (
	// aiViewCatalogMax keeps the catalog within ~4k tokens.
	aiViewCatalogMax = 16 * 1024
	// aiViewTextRows is how many rows of a shown view the model reads.
	aiViewTextRows = 20
	aiViewCellMax  = 80
)

func aiViewTools(call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	spec := map[string]any{"type": "object", "description": "a view spec: {view, args, fields, filter: [{field, op, value}], " +
		"sort: [{field, desc}], group, limit, layout, density, title}; ids come from view_catalog"}
	return []ai.Tool{
		{
			Name: "view_catalog",
			Description: "List the views termocode can show the user (on the phone and in the chat): id, params, typed fields " +
				"(which can be sorted or filtered) and layouts. Filter by feature or search words. Use it before view_show.",
			Params:   map[string]any{"feature": str("one feature, such as git, ci, runs, db (optional)"), "query": str("words to find in view ids, titles and fields (optional)")},
			ReadOnly: true,
			Describe: func(a map[string]any) string {
				return "List views " + strings.TrimSpace(ai.ArgString(a, "feature")+" "+ai.ArgString(a, "query"))
			},
			Run: func(_ context.Context, a map[string]any) (string, error) {
				return aiViewCatalogText(webproto.Views(), strings.TrimSpace(ai.ArgString(a, "feature")), strings.TrimSpace(ai.ArgString(a, "query"))), nil
			},
		},
		{
			Name: "view_show",
			Description: "Show data to the user as a view block (table, list, card …) built from a view spec; you get its title, " +
				"total and the first 20 rows as text. To show data, prefer this over writing a table yourself. " +
				"A refusal names the allowed ids: fix the spec and call again.",
			Params:   map[string]any{"spec": spec},
			Required: []string{"spec"},
			ReadOnly: true,
			Validate: func(a map[string]any) error { _, _, err := aiViewShowArgs(a); return err },
			Describe: func(a map[string]any) string {
				s, _ := aiViewSpecArg(a)
				return "Show view " + s.View
			},
			Release: aiViewDrop,
			View:    func(a map[string]any, _ string) *blocks.Block { return aiViewTake(a) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				def, s, err := aiViewShowArgs(a)
				if err != nil {
					return "", err
				}
				if call == nil {
					return "", errors.New("views run in a chat of the laptop")
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					b, err := m.aiViewRun(def, s)
					if err != nil {
						return "", nil, err
					}
					aiViewKeep(a, &b)
					return aiViewBlockText(b), nil, nil
				})
			},
		},
		{
			Name: "view_save",
			Description: "Save a view spec on this laptop under a name, for every paired phone (and pin it to the phones' Home " +
				"when pin is true). The user is always asked first. Saves the spec only, never data.",
			Params: map[string]any{"spec": spec, "name": str("a short name, up to 60 characters"),
				"pin": map[string]any{"type": "boolean", "description": "also pin it to the Home of the paired phones"}},
			Required: []string{"spec", "name"},
			Mutating: true,
			Class:    "", // always asks, with no "Always allow"
			Validate: func(a map[string]any) error { _, _, _, err := aiViewSaveArgs(a); return err },
			Describe: func(a map[string]any) string {
				name := strings.TrimSpace(ai.ArgString(a, "name"))
				if aiArgBool(a, "pin") {
					return fmt.Sprintf("Save view %q and pin it to the phone's Home?", name)
				}
				return fmt.Sprintf("Save view %q for the phone?", name)
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				raw, name, pin, err := aiViewSaveArgs(a)
				if err != nil {
					return "", err
				}
				if call == nil {
					return "", errors.New("views are saved from a chat of the laptop")
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					out, err := m.aiViewSave(raw, name, pin)
					return out, nil, err
				})
			},
		},
	}
}

func aiArgBool(a map[string]any, k string) bool {
	switch v := a[k].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	}
	return false
}

// aiViewSpecArg reads the spec arg: an object, or the same as JSON text
// (some models send nested objects as strings). Unknown keys are refused.
func aiViewSpecArg(a map[string]any) (blocks.ViewSpec, error) {
	var raw []byte
	switch v := a["spec"].(type) {
	case map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return blocks.ViewSpec{}, errors.New("spec is not a view spec")
		}
		raw = b
	case string:
		raw = []byte(v)
	default:
		return blocks.ViewSpec{}, errors.New("spec is needed: {view, args, fields, filter, sort, group, limit, layout}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s blocks.ViewSpec
	if err := dec.Decode(&s); err != nil {
		return blocks.ViewSpec{}, fmt.Errorf("spec is not a view spec: %s", strings.TrimPrefix(err.Error(), "json: "))
	}
	if s.View == "" {
		return blocks.ViewSpec{}, errors.New("spec.view is needed (an id from view_catalog)")
	}
	return s, nil
}

var (
	aiViewSQLRe  = regexp.MustCompile(`(?i)\bselect\b.+\bfrom\b|\binsert\s+into\b|\bupdate\s+\S+\s+set\b|\bdelete\s+from\b|\b(drop|alter|truncate)\s+(table|database|schema|index|view)\b|\bunion\s+(all\s+)?select\b|;`)
	aiViewDiskRe = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
)

// aiViewSpecSafe refuses a spec that carries SQL, a path outside the
// workspace or a URL in an arg or a filter value: a view reads what its
// source gives, never a query or a place the model picked.
func aiViewSpecSafe(s blocks.ViewSpec) error {
	check := func(what, v string) error {
		switch {
		case strings.Contains(v, "://"):
			return fmt.Errorf("%s: no URL in a view spec", what)
		case strings.HasPrefix(v, "/"), strings.HasPrefix(v, "~"), strings.HasPrefix(v, `\`), strings.Contains(v, ".."), aiViewDiskRe.MatchString(v):
			return fmt.Errorf("%s: no path outside the workspace in a view spec", what)
		case aiViewSQLRe.MatchString(v):
			return fmt.Errorf("%s: no SQL in a view spec (filter on the view's fields instead)", what)
		}
		return nil
	}
	for k, v := range s.Args {
		if err := check("arg "+k, v); err != nil {
			return err
		}
	}
	for _, c := range s.Filter {
		if err := check("filter "+c.Field, c.Value); err != nil {
			return err
		}
		for _, v := range c.In {
			if err := check("filter "+c.Field, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// aiViewShowDef finds the view of s in defs and applies the source rule:
// view_show runs what the phone could run by itself (a topic view);
// a view of an AI tool or an MCP tool is refused with that tool's name,
// so the model calls the tool itself (and its card rules apply).
func aiViewShowDef(defs []webproto.ViewDef, s blocks.ViewSpec) (webproto.ViewDef, error) {
	var def webproto.ViewDef
	found := false
	var close []string
	feat, _, _ := strings.Cut(s.View, ".")
	for _, d := range defs {
		if d.ID == s.View {
			def, found = d, true
			break
		}
		if strings.HasPrefix(d.ID, s.View) || strings.HasPrefix(s.View, d.ID) || strings.HasPrefix(d.ID, feat+".") {
			if len(close) < 8 {
				close = append(close, d.ID)
			}
		}
	}
	if !found {
		if len(close) > 0 {
			return def, fmt.Errorf("unknown view %q; close ones: %s (view_catalog lists all)", s.View, strings.Join(close, ", "))
		}
		return def, fmt.Errorf("unknown view %q (view_catalog lists all)", s.View)
	}
	src := def.Source
	switch {
	case src.Tool != "":
		return def, fmt.Errorf("view %s comes from the %s tool: use the tool itself", def.ID, src.Tool)
	case src.MCPServer != "":
		name := "mcp." + src.MCPServer
		if src.MCPTool != "" {
			name += "." + src.MCPTool
		}
		return def, fmt.Errorf("view %s comes from the MCP tool %s: use the tool itself", def.ID, name)
	case src.Topic == "":
		return def, fmt.Errorf("view %s reads the laptop's %s: open it from the phone's Views menu", def.ID, src.Op)
	}
	return def, nil
}

// aiViewShowArgs is the checked view and spec of a view_show call.
func aiViewShowArgs(a map[string]any) (webproto.ViewDef, blocks.ViewSpec, error) {
	s, err := aiViewSpecArg(a)
	if err != nil {
		return webproto.ViewDef{}, s, err
	}
	if err := aiViewSpecSafe(s); err != nil {
		return webproto.ViewDef{}, s, err
	}
	def, err := aiViewShowDef(webproto.Views(), s)
	if err != nil {
		return def, s, err
	}
	if err := webproto.CheckViewSpec(&s, def); err != nil {
		return def, s, errors.New(webViewMsg(err))
	}
	return def, s, nil
}

// aiViewRun runs a topic view in-process, as view.run does for the phone.
func (m *Model) aiViewRun(def webproto.ViewDef, s blocks.ViewSpec) (blocks.Block, error) {
	if m.web == nil {
		return blocks.Block{}, errors.New("views need the web panel of this laptop; it is not running")
	}
	src, err := m.webViewTopicData(def.Source.Topic)
	if err != nil {
		return blocks.Block{}, fmt.Errorf("view %s is not available yet", def.ID)
	}
	b, err := webViewBlock(def, s, src)
	if err != nil {
		return blocks.Block{}, fmt.Errorf("view %s is not available yet", def.ID)
	}
	b.ID = "" // the agent gives it the call id
	return b, nil
}

// aiViewSaveArgs is the checked spec (as JSON), name and pin of a
// view_save call.
func aiViewSaveArgs(a map[string]any) (json.RawMessage, string, bool, error) {
	s, err := aiViewSpecArg(a)
	if err != nil {
		return nil, "", false, err
	}
	name := strings.TrimSpace(ai.ArgString(a, "name"))
	if name == "" {
		return nil, "", false, errors.New("name is needed")
	}
	if utf8.RuneCountInString(name) > webproto.MaxViewName {
		return nil, "", false, fmt.Errorf("name: at most %d characters", webproto.MaxViewName)
	}
	if err := aiViewSpecSafe(s); err != nil {
		return nil, "", false, err
	}
	if defs := webproto.Views(); len(defs) > 0 {
		def, ok := webproto.LookupView(s.View)
		if !ok {
			_, err := aiViewShowDef(defs, s)
			return nil, "", false, err
		}
		if err := webproto.CheckViewSpec(&s, def); err != nil {
			return nil, "", false, errors.New(webViewMsg(err))
		}
	}
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > webproto.MaxViewSpec {
		return nil, "", false, fmt.Errorf("spec: at most %d bytes", webproto.MaxViewSpec)
	}
	return raw, name, aiArgBool(a, "pin"), nil
}

// aiViewSave keeps the spec in views.json and pins it on every paired
// phone that is not a guest's (a phone whose Home is full is skipped).
func (m *Model) aiViewSave(raw json.RawMessage, name string, pin bool) (string, error) {
	vs, ds, err := webViewStores()
	if err != nil {
		return "", errors.New("no config folder")
	}
	v, err := vs.Save(webdevices.View{Name: name, Spec: raw})
	switch {
	case errors.Is(err, webdevices.ErrViewsFull):
		return "", fmt.Errorf("%d views are saved; the user deletes one first", webproto.MaxSavedViews)
	case err != nil:
		return "", errors.New("could not save the view")
	}
	out := fmt.Sprintf("Saved view %q (id %s).", v.Name, v.ID)
	if pin {
		devs, _ := ds.List()
		n, full := 0, 0
		for _, d := range devs {
			if d.Guest {
				continue
			}
			if _, err := ds.SetPin(d.ID, v.ID, true, webproto.MaxViewPins); errors.Is(err, webdevices.ErrPinsFull) {
				full++
			} else if err == nil {
				n++
			}
		}
		out += fmt.Sprintf(" Pinned on %d phone(s)", n)
		if full > 0 {
			out += fmt.Sprintf("; %d phone(s) have %d pins already", full, webproto.MaxViewPins)
		}
		out += "."
	}
	m.webAuditLine(webproto.OpViewSave, v.Name, "ai", "ok")
	return out, nil
}

// aiViewCatalogText is the registry as compact text for the model.
func aiViewCatalogText(defs []webproto.ViewDef, feature, query string) string {
	q := strings.Fields(strings.ToLower(query))
	var sb strings.Builder
	sb.WriteString("Views (spec for view_show: {view, args, fields, filter: [{field, op, value}], sort: [{field, desc}], group, limit, layout, density}; " +
		"* = required param or default layout; filter ops: eq ne gt lt ge le contains starts in empty not_empty):\n")
	shown, left := 0, 0
	for _, d := range defs {
		if feature != "" && d.Feature != feature {
			continue
		}
		if !aiViewMatches(d, q) {
			continue
		}
		line := aiViewLine(d)
		if sb.Len()+len(line) > aiViewCatalogMax-600 {
			left++
			continue
		}
		sb.WriteString(line)
		shown++
	}
	if shown == 0 && left == 0 {
		return "No view matches; call view_catalog with no feature or query for all."
	}
	if left > 0 {
		feats := strings.Join(webproto.ViewFeatures, ", ")
		fmt.Fprintf(&sb, "… %d more views: pass feature (one of %s) or query to narrow.\n", left, feats)
	}
	return sb.String()
}

func aiViewMatches(d webproto.ViewDef, words []string) bool {
	if len(words) == 0 {
		return true
	}
	var hay strings.Builder
	hay.WriteString(strings.ToLower(d.ID + " " + d.Title + " " + d.Feature))
	for _, f := range d.Fields {
		hay.WriteString(" " + strings.ToLower(f.ID+" "+f.Label))
	}
	h := hay.String()
	for _, w := range words {
		if !strings.Contains(h, w) {
			return false
		}
	}
	return true
}

func aiViewLine(d webproto.ViewDef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- %s — %s", d.ID, d.Title)
	switch {
	case d.Source.Tool != "":
		fmt.Fprintf(&b, " (from tool %s: call it directly)", d.Source.Tool)
	case d.Source.MCPServer != "":
		fmt.Fprintf(&b, " (from MCP %s %s: call the tool directly)", d.Source.MCPServer, d.Source.MCPTool)
	case d.Live:
		b.WriteString(" (live)")
	}
	if len(d.Params) > 0 {
		b.WriteString("; params:")
		for _, p := range d.Params {
			req := ""
			if p.Required {
				req = "*"
			}
			fmt.Fprintf(&b, " %s%s(%s", p.ID, req, p.Kind)
			if len(p.Enum) > 0 {
				b.WriteString(": " + strings.Join(p.Enum, "|"))
			}
			b.WriteString(")")
		}
	}
	b.WriteString("; fields:")
	for _, f := range d.Fields {
		fmt.Fprintf(&b, " %s:%s", f.ID, f.Kind)
		var can []string
		if f.Sortable {
			can = append(can, "sort")
		}
		if f.Filterable {
			can = append(can, "filter")
		}
		if len(can) > 0 {
			b.WriteString("[" + strings.Join(can, ",") + "]")
		}
	}
	b.WriteString("; layouts:")
	for i, l := range d.Layouts {
		if i == 0 {
			l += "*"
		}
		b.WriteString(" " + l)
	}
	b.WriteString("\n")
	return b.String()
}

// aiViewBlockText is what the model reads of a shown view: the title,
// the total and at most aiViewTextRows rows or items.
func aiViewBlockText(b blocks.Block) string {
	var sb strings.Builder
	sb.WriteString("Shown to the user: " + b.Title)
	cut := func(s string) string {
		if utf8.RuneCountInString(s) <= aiViewCellMax {
			return s
		}
		r := []rune(s)
		return string(r[:aiViewCellMax-1]) + "…"
	}
	n := 0
	rows := func(rs [][]string) {
		for _, r := range rs {
			if n >= aiViewTextRows {
				return
			}
			cells := make([]string, len(r))
			for i, c := range r {
				cells[i] = cut(c)
			}
			sb.WriteString(strings.Join(cells, " | ") + "\n")
			n++
		}
	}
	items := func(is []blocks.BlockItem) {
		for _, it := range is {
			if n >= aiViewTextRows {
				return
			}
			parts := []string{cut(it.Title)}
			for _, p := range []string{it.Sub, it.Badge, it.Tone} {
				if p != "" {
					parts = append(parts, cut(p))
				}
			}
			sb.WriteString("- " + strings.Join(parts, " · ") + "\n")
			n++
		}
	}
	all := len(b.Rows) + len(b.Items)
	for _, g := range b.Groups {
		all += len(g.Rows) + len(g.Items)
	}
	total := b.Total
	if total < all {
		total = all
	}
	if total > 0 {
		fmt.Fprintf(&sb, " (total %d", total)
		if total > aiViewTextRows {
			fmt.Fprintf(&sb, ", first %d here", aiViewTextRows)
		}
		sb.WriteString(")")
	}
	sb.WriteString("\n")
	if len(b.Columns) > 0 {
		labels := make([]string, len(b.Columns))
		for i, c := range b.Columns {
			labels[i] = c.Label
		}
		sb.WriteString(strings.Join(labels, " | ") + "\n")
	}
	rows(b.Rows)
	items(b.Items)
	for _, g := range b.Groups {
		if n >= aiViewTextRows {
			break
		}
		fmt.Fprintf(&sb, "%s (%d):\n", g.Label, g.Count)
		rows(g.Rows)
		items(g.Items)
	}
	for _, f := range b.Fields {
		fmt.Fprintf(&sb, "%s: %s\n", f.Label, cut(f.Value))
	}
	if b.State != "" {
		sb.WriteString("state: " + b.State + "\n")
	}
	if b.Diff != nil {
		fmt.Fprintf(&sb, "diff %s +%d −%d\n", b.Diff.Path, b.Diff.Adds, b.Diff.Dels)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// The block a view_show call built waits here, keyed by the call's args
// map, until the View hook takes it (or Release drops it).
var aiViewBlocks sync.Map

func aiViewKey(a map[string]any) uintptr { return reflect.ValueOf(a).Pointer() }

func aiViewKeep(a map[string]any, b *blocks.Block) { aiViewBlocks.Store(aiViewKey(a), b) }

func aiViewTake(a map[string]any) *blocks.Block {
	v, ok := aiViewBlocks.LoadAndDelete(aiViewKey(a))
	if !ok {
		return nil
	}
	return v.(*blocks.Block)
}

func aiViewDrop(a map[string]any) { aiViewBlocks.Delete(aiViewKey(a)) }
