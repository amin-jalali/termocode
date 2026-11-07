package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Default views for MCP tools and resources (MOB-72, mobile plan
// 3.11.6). Built from what the Manager already knows (KnownTools and the
// tool cache), never starting a server:
//   - params from the tool's scalar input properties; an object or array
//     input makes the view "Ask the AI" only;
//   - fields from the output schema, else learned from the first good
//     result (structuredContent, or text that parses as JSON) and kept
//     in the tool cache, so the view is typed from the second call on;
//   - the run rule: only a read-only, not turned off, in-profile tool
//     with mcp_trust_annotations on (and an accepted tool list) runs
//     directly; every other view opens as "Ask the AI", where the AI's
//     cards apply;
//   - one resource view per server: a uri picker from its resources and
//     templates; the content's shape picks the layout.
// The phone's view spec (MOB-9) wraps these; this file holds no
// transport.

// View layouts.
const (
	ViewTable    = "table"
	ViewList     = "list"
	ViewCard     = "card"
	ViewDetail   = "detail"
	ViewText     = "text"
	ViewMarkdown = "markdown"
	ViewJSON     = "json"
	ViewLine     = "line" // one line: binary or too big to show
)

// Field and param kinds.
const (
	KindString  = "string"
	KindNumber  = "number"
	KindInteger = "integer"
	KindBool    = "bool"
	KindTime    = "time"
	KindJSON    = "json" // a nested object or array, shown short ("…" opens it)
	KindEnum    = "enum"
)

// MaxViewFields caps a view's fields; MaxResourceView is the biggest
// resource text a view shows (bigger is one line).
const (
	MaxViewFields   = 32
	MaxResourceView = 32 << 10
	maxViewArg      = 4 << 10
	maxResourceURIs = 200
)

// ViewParam is one input of a tool view.
type ViewParam struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Enum        []string `json:"enum,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
}

// ViewField is one column (table) or row (card) of a view.
type ViewField struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ViewShape is how a tool's result is drawn: the layout, the fields and,
// for an object that wraps one list ({"issues": […]}), the list's key.
type ViewShape struct {
	Layout string      `json:"layout"`
	Fields []ViewField `json:"fields,omitempty"`
	Path   string      `json:"path,omitempty"`
}

// ToolView is the generated default view of one MCP tool.
type ToolView struct {
	ID          string // mcp.<server>.<tool>
	Server      string
	Tool        string
	Title       string
	Description string
	Params      []ViewParam
	Shape       ViewShape
	Typed       bool // the fields are known (output schema or learned)
	Learned     bool // the fields were learned from a result
	Direct      bool // runs from the menu or a spec; else "Ask the AI"
	Why         string
}

// ViewRule is the read-only run rule's inputs.
type ViewRule struct {
	TrustAnnotations bool                           // mcp_trust_annotations
	Off              func(server, tool string) bool // mcp_disabled_tools (Settings.ToolOff); nil: none
	InProfile        func(server, tool string) bool // the chat's MCP profile; nil: every tool
}

// ViewParamsFromSchema turns the scalar input properties into params,
// sorted by name. askOnly: an input that is not a flat list of scalars
// (an object, an array or an unknown type), so only the AI can call it.
func ViewParamsFromSchema(in map[string]any) (params []ViewParam, askOnly bool) {
	pr, _ := in["properties"].(map[string]any)
	if len(pr) == 0 {
		return nil, false
	}
	req := map[string]bool{}
	if r, ok := in["required"].([]any); ok {
		for _, x := range r {
			if n, ok := x.(string); ok {
				req[n] = true
			}
		}
	}
	for _, name := range viewKeys(pr) {
		p, _ := pr[name].(map[string]any)
		kind, enum := scalarKind(p)
		if name == "" || kind == "" {
			return nil, true
		}
		d, _ := p["description"].(string)
		params = append(params, ViewParam{Name: name, Kind: kind, Enum: enum, Required: req[name], Description: d})
	}
	return params, false
}

// scalarKind is a param's kind, or "" when it is not a scalar.
func scalarKind(p map[string]any) (string, []string) {
	if p == nil {
		return "", nil
	}
	if e, ok := p["enum"].([]any); ok {
		var out []string
		for _, x := range e {
			s, ok := x.(string)
			if !ok {
				return "", nil
			}
			out = append(out, s)
		}
		if len(out) > 0 {
			return KindEnum, out
		}
		return "", nil
	}
	switch schemaType(p) {
	case "string":
		return KindString, nil
	case "integer":
		return KindInteger, nil
	case "number":
		return KindNumber, nil
	case "boolean":
		return KindBool, nil
	}
	return "", nil
}

// schemaType is a schema's type: "type" as a string, the one non-null
// entry of a type list, or the one non-null branch of anyOf / oneOf.
func schemaType(p map[string]any) string {
	pick := func(types []string) string {
		var got []string
		for _, t := range types {
			if t != "null" {
				got = append(got, t)
			}
		}
		if len(got) == 1 {
			return got[0]
		}
		return ""
	}
	switch t := p["type"].(type) {
	case string:
		return t
	case []any:
		var ts []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				ts = append(ts, s)
			}
		}
		return pick(ts)
	}
	for _, k := range []string{"anyOf", "oneOf"} {
		if br, ok := p[k].([]any); ok {
			var ts []string
			for _, b := range br {
				if m, ok := b.(map[string]any); ok {
					ts = append(ts, schemaType(m))
				}
			}
			return pick(ts)
		}
	}
	if _, ok := p["properties"].(map[string]any); ok {
		return "object"
	}
	return ""
}

// ViewShapeFromSchema reads the fields from an output schema. typed:
// the schema said enough to draw the result.
func ViewShapeFromSchema(out map[string]any) (ViewShape, bool) {
	switch schemaType(out) {
	case "array":
		item, _ := out["items"].(map[string]any)
		if f := schemaFields(item); len(f) > 0 {
			return ViewShape{Layout: ViewTable, Fields: f}, true
		}
		return ViewShape{Layout: ViewList}, true
	case "object":
		pr, _ := out["properties"].(map[string]any)
		if len(pr) == 1 {
			for k, v := range pr {
				if m, ok := v.(map[string]any); ok && schemaType(m) == "array" && k != "" {
					item, _ := m["items"].(map[string]any)
					if f := schemaFields(item); len(f) > 0 {
						return ViewShape{Layout: ViewTable, Fields: f, Path: k}, true
					}
				}
			}
		}
		if f := schemaFields(out); len(f) > 0 {
			return ViewShape{Layout: ViewCard, Fields: f}, true
		}
	case "string":
		return ViewShape{Layout: ViewText}, true
	}
	return ViewShape{Layout: ViewJSON}, false
}

// schemaFields are an object schema's properties as fields, by name.
func schemaFields(obj map[string]any) []ViewField {
	pr, _ := obj["properties"].(map[string]any)
	var out []ViewField
	for _, name := range viewKeys(pr) {
		if name == "" || len(out) == MaxViewFields {
			continue
		}
		p, _ := pr[name].(map[string]any)
		kind := KindJSON
		switch schemaType(p) {
		case "string":
			kind = KindString
			if f, _ := p["format"].(string); f == "date-time" {
				kind = KindTime
			}
		case "integer":
			kind = KindInteger
		case "number":
			kind = KindNumber
		case "boolean":
			kind = KindBool
		}
		out = append(out, ViewField{Name: name, Kind: kind})
	}
	return out
}

// LearnViewShape reads a shape from a result: the keys of the first
// object, in the order the server sent them, kinds by value. ok is false
// for an error or an empty result (nothing to learn).
func LearnViewShape(res CallResult) (ViewShape, bool) {
	if res.IsError {
		return ViewShape{}, false
	}
	raw := bytes.TrimSpace(res.StructuredContent)
	if len(raw) == 0 || !json.Valid(raw) {
		raw = nil
		for _, c := range res.Content {
			if c.Type != "text" || strings.TrimSpace(c.Text) == "" {
				continue
			}
			t := bytes.TrimSpace([]byte(c.Text))
			if (t[0] == '[' || t[0] == '{') && json.Valid(t) {
				raw = t
				break
			}
			return ViewShape{Layout: ViewText}, true
		}
	}
	if len(raw) == 0 {
		return ViewShape{}, false
	}
	switch raw[0] {
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
			return ViewShape{}, false
		}
		first := bytes.TrimSpace(items[0])
		if len(first) > 0 && first[0] == '{' {
			if f := valueFields(first); len(f) > 0 {
				return ViewShape{Layout: ViewTable, Fields: f}, true
			}
			return ViewShape{}, false
		}
		if len(first) > 0 && first[0] == '[' {
			return ViewShape{Layout: ViewJSON}, true
		}
		return ViewShape{Layout: ViewList}, true
	case '{':
		keys, vals, ok := viewObjectKeys(raw)
		if !ok {
			return ViewShape{}, false
		}
		if len(keys) == 1 && keys[0] != "" {
			var items []json.RawMessage
			if json.Unmarshal(vals[0], &items) == nil && len(items) > 0 {
				if first := bytes.TrimSpace(items[0]); len(first) > 0 && first[0] == '{' {
					if f := valueFields(first); len(f) > 0 {
						return ViewShape{Layout: ViewTable, Fields: f, Path: keys[0]}, true
					}
				}
			}
		}
		if f := valueFields(raw); len(f) > 0 {
			return ViewShape{Layout: ViewCard, Fields: f}, true
		}
		return ViewShape{}, false
	case '"':
		return ViewShape{Layout: ViewText}, true
	}
	return ViewShape{Layout: ViewJSON}, true
}

// valueFields are an object's keys as fields, first-seen order, each
// name once.
func valueFields(obj []byte) []ViewField {
	keys, vals, ok := viewObjectKeys(obj)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []ViewField
	for i, k := range keys {
		if k == "" || seen[k] || len(out) == MaxViewFields {
			continue
		}
		seen[k] = true
		out = append(out, ViewField{Name: k, Kind: valueKind(vals[i])})
	}
	return out
}

func valueKind(v json.RawMessage) string {
	v = bytes.TrimSpace(v)
	if len(v) == 0 {
		return KindJSON
	}
	switch v[0] {
	case '"':
		var s string
		if json.Unmarshal(v, &s) == nil {
			if _, err := time.Parse(time.RFC3339, s); err == nil {
				return KindTime
			}
		}
		return KindString
	case 't', 'f':
		return KindBool
	case 'n':
		return KindJSON
	case '{', '[':
		return KindJSON
	}
	return KindNumber
}

// viewObjectKeys reads a JSON object's keys and values in the order sent.
func viewObjectKeys(raw []byte) ([]string, []json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, false
	}
	var keys []string
	var vals []json.RawMessage
	for dec.More() {
		kt, err := dec.Token()
		k, ok := kt.(string)
		if err != nil || !ok {
			return nil, nil, false
		}
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			return nil, nil, false
		}
		keys, vals = append(keys, k), append(vals, v)
	}
	return keys, vals, true
}

// ToolViewOf builds a tool's default view. learned is the shape a
// result taught (nil: none); toolsChanged: the server's tool list
// differs from the trusted one. shown is false for a tool the server's
// entry hides (deny list, or a read-only server's write tool).
func ToolViewOf(s Server, t Tool, learned *ViewShape, rule ViewRule, toolsChanged bool) (ToolView, bool) {
	if exposed, _, _ := Gate(s, t, rule.TrustAnnotations); !exposed {
		return ToolView{}, false
	}
	params, askOnly := ViewParamsFromSchema(t.InputSchema)
	shape, typed := ViewShapeFromSchema(t.OutputSchema)
	v := ToolView{ID: "mcp." + s.Name + "." + t.Name, Server: s.Name, Tool: t.Name, Title: toolTitle(t),
		Description: t.Description, Params: params, Shape: shape, Typed: typed}
	if !typed && learned != nil {
		v.Shape, v.Typed, v.Learned = *learned, true, true
	}
	switch {
	case !t.ReadOnly():
		v.Why = "the tool is not read-only"
	case !rule.TrustAnnotations:
		v.Why = "mcp_trust_annotations is off"
	case rule.Off != nil && rule.Off(s.Name, t.Name):
		v.Why = "the tool is turned off"
	case rule.InProfile != nil && !rule.InProfile(s.Name, t.Name):
		v.Why = "the tool is outside the chat's MCP profile"
	case toolsChanged:
		v.Why = "the server's tools changed since you trusted them"
	case askOnly:
		v.Why = "its input is more than simple values"
	}
	v.Direct = v.Why == ""
	return v, true
}

func toolTitle(t Tool) string {
	if t.Title != "" {
		return t.Title
	}
	if t.Annotations != nil && t.Annotations.Title != "" {
		return t.Annotations.Title
	}
	return t.Name
}

// CheckViewArgs checks the args of a direct run on the laptop: only the
// view's params, the required ones present, each of its kind, and no
// secret-looking value (that is typed on the laptop, never sent from the
// phone).
func CheckViewArgs(v ToolView, args map[string]any) error {
	if !v.Direct {
		return fmt.Errorf("%s runs through the AI only: %s", v.ID, v.Why)
	}
	byName := map[string]ViewParam{}
	for _, p := range v.Params {
		byName[p.Name] = p
	}
	for _, name := range viewKeys(args) {
		p, ok := byName[name]
		if !ok {
			return fmt.Errorf("%s has no param %s", v.ID, name)
		}
		if err := checkViewArg(p, args[name]); err != nil {
			return err
		}
	}
	for _, p := range v.Params {
		if _, ok := args[p.Name]; p.Required && !ok {
			return fmt.Errorf("%s needs %s", v.ID, p.Name)
		}
	}
	return nil
}

func checkViewArg(p ViewParam, x any) error {
	bad := fmt.Errorf("%s must be a %s", p.Name, p.Kind)
	switch p.Kind {
	case KindString, KindEnum:
		s, ok := x.(string)
		if !ok {
			return bad
		}
		if len(s) > maxViewArg {
			return fmt.Errorf("%s is too long", p.Name)
		}
		if ai.Redact(s) != s {
			return fmt.Errorf("%s looks like a secret: it stays on the laptop, type it there", p.Name)
		}
		if p.Kind == KindEnum {
			for _, e := range p.Enum {
				if e == s {
					return nil
				}
			}
			return fmt.Errorf("%s must be one of %s", p.Name, strings.Join(p.Enum, ", "))
		}
	case KindInteger:
		f, ok := x.(float64)
		if !ok || f != math.Trunc(f) || math.IsInf(f, 0) {
			return bad
		}
	case KindNumber:
		f, ok := x.(float64)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return bad
		}
	case KindBool:
		if _, ok := x.(bool); !ok {
			return bad
		}
	default:
		return bad
	}
	return nil
}

// ResourceChoice is one resource in a resource view's uri picker.
type ResourceChoice struct {
	URI      string
	Label    string
	MimeType string
}

// ResourceTemplateView is one template in the picker, with the
// variables the user fills in.
type ResourceTemplateView struct {
	URITemplate string
	Label       string
	Vars        []string
}

// ResourceView is a server's one resource view (param uri).
type ResourceView struct {
	ID        string // mcp.<server>.resource
	Server    string
	Title     string
	URIs      []ResourceChoice
	Templates []ResourceTemplateView
}

// ResourceViewOf builds a server's resource view from its lists.
func ResourceViewOf(server string, rs []Resource, ts []ResourceTemplate) ResourceView {
	v := ResourceView{ID: "mcp." + server + ".resource", Server: server, Title: server + " resources"}
	for _, r := range rs {
		if len(v.URIs) == maxResourceURIs {
			break
		}
		v.URIs = append(v.URIs, ResourceChoice{URI: r.URI, Label: r.Label(), MimeType: r.MimeType})
	}
	for _, t := range ts {
		if len(v.Templates) == maxResourceURIs {
			break
		}
		label := t.Title
		if label == "" {
			label = t.Name
		}
		v.Templates = append(v.Templates, ResourceTemplateView{URITemplate: t.URITemplate, Label: label, Vars: TemplateVars(t.URITemplate)})
	}
	return v
}

// ResourceViewLayout picks a resource's layout by its shape: text (or
// markdown by mime or name), a JSON array as a table, a JSON object as
// detail, binary or over MaxResourceView as one line.
func ResourceViewLayout(c ResourceContents) string {
	if c.Blob != "" || len(c.Text) > MaxResourceView {
		return ViewLine
	}
	mime := strings.ToLower(c.MimeType)
	if strings.Contains(mime, "markdown") || (mime == "" && strings.HasSuffix(strings.ToLower(c.URI), ".md")) {
		return ViewMarkdown
	}
	if t := strings.TrimSpace(c.Text); t != "" && json.Valid([]byte(t)) {
		switch t[0] {
		case '[':
			return ViewTable
		case '{':
			return ViewDetail
		}
	}
	return ViewText
}

// TemplateVars are the variable names of a URI template (RFC 6570), in
// order, each once: "{owner}", "{+path}", "{?state,page}", "{id:3}".
func TemplateVars(tpl string) []string {
	var out []string
	seen := map[string]bool{}
	for {
		i := strings.IndexByte(tpl, '{')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(tpl[i:], '}')
		if j < 0 {
			return out
		}
		expr := strings.TrimLeft(tpl[i+1:i+j], "+#./;?&")
		for _, v := range strings.Split(expr, ",") {
			if k := strings.IndexByte(v, ':'); k >= 0 {
				v = v[:k]
			}
			v = strings.TrimSuffix(strings.TrimSpace(v), "*")
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		tpl = tpl[i+j+1:]
	}
}

func viewKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func copyViews(v map[string]ViewShape) map[string]ViewShape {
	if len(v) == 0 {
		return nil
	}
	out := make(map[string]ViewShape, len(v))
	for k, s := range v {
		out[k] = s
	}
	return out
}

// ── Manager ───────────────────────────────────────────────────────────

// readViewsLocked reads the learned shapes from the tool cache once.
func (m *Manager) readViewsLocked(st *serverState) {
	if st.viewsRead {
		return
	}
	st.viewsRead = true
	p := m.cachePath(st.cfg.Name)
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var c cacheFile
	if json.Unmarshal(b, &c) != nil || c.Hash != st.cfg.Hash() {
		return
	}
	for k, s := range c.Views {
		if st.views == nil {
			st.views = map[string]ViewShape{}
		}
		st.views[k] = s
	}
}

// ToolViews lists the default views of every server's known tools, by
// server then tool name. It never starts a server: a server whose tools
// were never listed has no views yet.
func (m *Manager) ToolViews(rule ViewRule) []ToolView {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ToolView
	for _, name := range viewKeys(m.servers) {
		st := m.servers[name]
		if st.cfg.Disabled {
			continue
		}
		m.loadCacheLocked(st)
		if !st.haveTools {
			continue
		}
		m.readViewsLocked(st)
		changed := m.toolsChangedLocked(st) && st.toolsOK != st.toolsHash
		tools := append([]Tool(nil), st.tools...)
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		for _, t := range tools {
			var learned *ViewShape
			if s, ok := st.views[t.Name]; ok {
				learned = &s
			}
			if v, shown := ToolViewOf(st.cfg, t, learned, rule, changed); shown {
				out = append(out, v)
			}
		}
	}
	return out
}

// ResourceViews lists one resource view per server whose known
// capabilities include resources. The picker is empty here: the lists
// are asked when the view runs (that may start the server).
func (m *Manager) ResourceViews() []ResourceView {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ResourceView
	for _, name := range viewKeys(m.servers) {
		st := m.servers[name]
		if st.cfg.Disabled {
			continue
		}
		m.loadCacheLocked(st)
		if st.caps.Resources {
			out = append(out, ResourceViewOf(name, nil, nil))
		}
	}
	return out
}

// LearnViewResult keeps the shape of a tool's first good result (a tool
// with an output schema needs none). It goes into the tool cache unless
// the server's list is private.
func (m *Manager) LearnViewResult(server, tool string, res CallResult) {
	shape, ok := LearnViewShape(res)
	if !ok {
		return
	}
	m.mu.Lock()
	st, found := m.servers[server]
	if !found {
		m.mu.Unlock()
		return
	}
	m.readViewsLocked(st)
	known := false
	for _, t := range st.tools {
		if t.Name == tool {
			known = t.OutputSchema == nil
			break
		}
	}
	if _, have := st.views[tool]; have || !known {
		m.mu.Unlock()
		return
	}
	if st.views == nil {
		st.views = map[string]ViewShape{}
	}
	st.views[tool] = shape
	cfg, views, private := st.cfg, copyViews(st.views), st.private
	m.mu.Unlock()
	if !private {
		m.saveViews(cfg, views)
	}
}

// saveViews writes the learned shapes into the server's existing tool
// cache file (one written by a tool list, for this entry).
func (m *Manager) saveViews(cfg Server, views map[string]ViewShape) {
	p := m.cachePath(cfg.Name)
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var c cacheFile
	if json.Unmarshal(b, &c) != nil || c.Hash != cfg.Hash() {
		return
	}
	c.Views = views
	if out, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(p, out, 0o644)
	}
}
