package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Default views for MCP tools and resources (MOBUX-14, mobile plan
// §3.11.6). Every known tool of a server gets a view the phone can open
// without the AI: params from the tool's scalar input properties, fields
// from its outputSchema or learned from the first structured result, a
// layout by shape (table, list, card, detail, text, json). The raw JSON
// of a result is always kept beside the shaped data, so the phone shows
// it one tap away. Views are built from the known tool list only: making
// one never starts a server.

// View limits.
const (
	MaxViewParams     = 12       // params per view (scalars only)
	MaxViewFields     = 40       // fields per view (detail)
	MaxTableFields    = 12       // columns of a table
	MaxViewRows       = 200      // rows shown
	MaxViewCell       = 200      // runes per cell
	MaxViewJSON       = 16 << 10 // raw JSON kept for the "{ } Raw" tap
	MaxViewText       = 32 << 10 // a text / resource result
	MaxLearnedTools   = 64       // tools with learned fields per server
	maxViewEnum       = 32       // enum values of one param
	maxResourceChoice = 100      // resources offered in the uri picker
	cardMaxFields     = 6        // an object this small is a card, else a detail
	viewIDPrefix      = "mcp."   // view ids: mcp.<server>.<tool>
)

// ResourceViewTool is the tool part of a server's resource view id
// (mcp.<server>.@resources); "@" is never in an MCP tool name, so it
// cannot clash with a real tool.
const ResourceViewTool = "@resources"

// View layouts.
const (
	LayoutTable  = "table"
	LayoutList   = "list"
	LayoutCard   = "card"
	LayoutDetail = "detail"
	LayoutText   = "text"
	LayoutJSON   = "json"
	LayoutStatus = "status" // a tool error
)

// Field kinds (the view spec's ViewField kinds).
const (
	FieldText = "text"
	FieldNum  = "num"
	FieldTime = "time"
	FieldBool = "bool"
	FieldLink = "link"
	FieldJSON = "json" // a nested object or array: a short value, "…" opens it
)

// ViewParam is one input of a view (a scalar input property).
type ViewParam struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"` // string|int|number|bool|enum|uri
	Enum     []string `json:"enum,omitempty"`
	Required bool     `json:"required,omitempty"`
	Hint     string   `json:"hint,omitempty"` // the property's description, one line
}

// ViewField is one field (a column, or a row of a detail).
type ViewField struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// ToolView is the generated default view of one MCP tool (or of a
// server's resources).
type ToolView struct {
	ID          string      `json:"id"` // mcp.<server>.<tool>
	Server      string      `json:"server"`
	Tool        string      `json:"tool"` // ResourceViewTool for the resource view
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Params      []ViewParam `json:"params,omitempty"`
	Fields      []ViewField `json:"fields,omitempty"`
	Layout      string      `json:"layout"` // the default layout
	Typed       string      `json:"typed,omitempty" enum:"schema|learned"`
	// Run: the phone may run it directly (read-only, trusted). Else the
	// view opens as "Ask the AI" and Why says why, in one line.
	Run bool   `json:"run,omitempty"`
	Why string `json:"why,omitempty"`
	// Resources is the uri picker of the resource view.
	Resources []ResourceChoice `json:"resources,omitempty"`
}

// ResourceChoice is one row of the resource view's uri picker: a
// resource, or a template with its variables.
type ResourceChoice struct {
	URI      string   `json:"uri"`
	Name     string   `json:"name"`
	Template bool     `json:"template,omitempty"`
	Vars     []string `json:"vars,omitempty"`
}

// ViewRule is what decides whether a view runs from the phone.
type ViewRule struct {
	TrustAnnotations bool     // mcp_trust_annotations
	Profile          *Profile // the chat's MCP profile (nil: all servers)
	// Off reports a tool the user turned off (mcp_disabled_tools); nil:
	// none is off.
	Off func(server, tool string) bool
}

// ToolViewID is the view id of a tool.
func ToolViewID(server, tool string) string { return viewIDPrefix + server + "." + tool }

// ParseToolViewID splits a view id; ok false when it is not an MCP view.
// A server name has no dot (config.go), so the first dot after the
// prefix ends it.
func ParseToolViewID(id string) (server, tool string, ok bool) {
	rest, found := strings.CutPrefix(id, viewIDPrefix)
	if !found {
		return "", "", false
	}
	server, tool, found = strings.Cut(rest, ".")
	if !found || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

// ToolViews builds the default views of a server's known tools, in the
// server's order, plus its resource view when it has resources. learned
// holds the fields learned from earlier results (LearnedFields).
func ToolViews(s Server, tools []Tool, caps ServerCaps, learned map[string]Learned, rule ViewRule) []ToolView {
	out := make([]ToolView, 0, len(tools)+1)
	for _, t := range tools {
		if matchAny(s.Deny, t.Name) {
			continue // denied tools are not offered at all
		}
		out = append(out, toolView(s, t, learned[t.Name], rule))
	}
	if caps.Resources {
		rv := ResourceView(s, nil, nil)
		if rv.Run && rule.Profile != nil && !rule.Profile.AllowsServer(s.Name) {
			rv.Run, rv.Why = false, "not in the chat's MCP profile"
		}
		out = append(out, rv)
	}
	return out
}

func toolView(s Server, t Tool, learned Learned, rule ViewRule) ToolView {
	v := ToolView{ID: ToolViewID(s.Name, t.Name), Server: s.Name, Tool: t.Name, Title: toolTitle(t), Description: oneLine(t.Description, 200)}
	params, complexInput := viewParams(t.InputSchema)
	v.Params = params
	if f, layout, ok := schemaFields(t.OutputSchema); ok {
		v.Fields, v.Layout, v.Typed = f, layout, "schema"
	} else if len(learned.Fields) > 0 {
		v.Fields, v.Layout, v.Typed = capFields(learned.Fields, MaxViewFields), learned.Layout, "learned"
		if v.Layout == "" {
			v.Layout = LayoutTable
		}
	} else {
		v.Layout = LayoutJSON // unknown → json, always
	}
	v.Run, v.Why = runnable(s, t, rule, complexInput)
	return v
}

// runnable is the read-only run rule: only a tool the server marks
// read-only, not disabled or denied, inside the profile, with
// mcp_trust_annotations on, and with scalar inputs only, runs from the
// phone; any other opens as "Ask the AI" (the AI's card gates apply).
func runnable(s Server, t Tool, rule ViewRule, complexInput bool) (bool, string) {
	switch {
	case s.Disabled:
		return false, "the server is turned off"
	case rule.Off != nil && rule.Off(s.Name, t.Name):
		return false, "the tool is turned off"
	case rule.Profile != nil && !rule.Profile.Allows(s.Name, t.Name):
		return false, "not in the chat's MCP profile"
	case !t.ReadOnly():
		return false, "the tool can change things: Ask the AI"
	case !rule.TrustAnnotations:
		return false, "read-only hints are not trusted (mcp_trust_annotations): Ask the AI"
	case complexInput:
		return false, "the tool takes an object or list: Ask the AI"
	}
	return true, ""
}

func toolTitle(t Tool) string {
	switch {
	case t.Title != "":
		return oneLine(t.Title, 60)
	case t.Annotations != nil && t.Annotations.Title != "":
		return oneLine(t.Annotations.Title, 60)
	}
	return oneLine(humanize(t.Name), 60)
}

// viewParams turns the scalar input properties into params, in name
// order with required ones first; complex reports a required object or
// array input (the tool then opens as "Ask the AI").
func viewParams(schema map[string]any) (params []ViewParam, complexInput bool) {
	props, _ := schema["properties"].(map[string]any)
	req := map[string]bool{}
	if r, ok := schema["required"].([]any); ok {
		for _, x := range r {
			if s, ok := x.(string); ok {
				req[s] = true
			}
		}
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		if req[names[i]] != req[names[j]] {
			return req[names[i]]
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		p, _ := props[name].(map[string]any)
		kind := paramKind(p)
		if kind == "" {
			if req[name] {
				complexInput = true
			}
			continue
		}
		if len(params) == MaxViewParams {
			if req[name] {
				complexInput = true // a required input the phone cannot fill
			}
			continue
		}
		vp := ViewParam{ID: name, Label: humanize(name), Kind: kind, Required: req[name], Hint: oneLine(str(p["description"]), 120)}
		if kind == "enum" {
			vp.Enum = enumOf(p)
		}
		params = append(params, vp)
	}
	return params, complexInput
}

// paramKind is the param kind of a property; "" for an object, an array
// or anything else the phone does not fill.
func paramKind(p map[string]any) string {
	if p == nil {
		return ""
	}
	if e := enumOf(p); len(e) > 0 {
		return "enum"
	}
	switch firstType(p["type"]) {
	case "string":
		if f := str(p["format"]); f == "uri" || f == "uri-reference" {
			return "uri"
		}
		return "string"
	case "integer":
		return "int"
	case "number":
		return "number"
	case "boolean":
		return "bool"
	}
	return ""
}

func enumOf(p map[string]any) []string {
	raw, _ := p["enum"].([]any)
	var out []string
	for _, x := range raw {
		if len(out) == maxViewEnum {
			break
		}
		switch v := x.(type) {
		case string:
			out = append(out, v)
		case float64, bool:
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
}

// firstType reads "type": "x" or "type": ["x", "null"] (the first
// non-null).
func firstType(t any) string {
	switch v := t.(type) {
	case string:
		return v
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && s != "null" {
				return s
			}
		}
	}
	return ""
}

// schemaFields reads the fields and layout from an outputSchema: an
// array's item properties (table), an object holding one array of
// objects (table over it), or an object's properties (card / detail).
func schemaFields(schema map[string]any) ([]ViewField, string, bool) {
	if schema == nil {
		return nil, "", false
	}
	switch firstType(schema["type"]) {
	case "array":
		items, _ := schema["items"].(map[string]any)
		if f := propFields(items, MaxTableFields); len(f) > 0 {
			return f, LayoutTable, true
		}
		return []ViewField{{ID: "value", Label: "Value", Kind: FieldText}}, LayoutList, true
	case "object", "":
		props, _ := schema["properties"].(map[string]any)
		if len(props) == 0 {
			return nil, "", false
		}
		if key, items := wrappedArraySchema(props); key != "" {
			if f := propFields(items, MaxTableFields); len(f) > 0 {
				return f, LayoutTable, true
			}
		}
		f := propFields(schema, MaxViewFields)
		if len(f) == 0 {
			return nil, "", false
		}
		if len(f) <= cardMaxFields {
			return f, LayoutCard, true
		}
		return f, LayoutDetail, true
	}
	return nil, "", false
}

// wrappedArraySchema finds the one property that is an array of
// objects (MCP's structuredContent is an object, so lists come wrapped:
// {"issues": [...], "total": 3}).
func wrappedArraySchema(props map[string]any) (string, map[string]any) {
	key, n := "", 0
	var items map[string]any
	for k, v := range props {
		p, _ := v.(map[string]any)
		if firstType(p["type"]) != "array" {
			continue
		}
		it, _ := p["items"].(map[string]any)
		if firstType(it["type"]) == "object" || it["properties"] != nil {
			key, items, n = k, it, n+1
		}
	}
	if n != 1 {
		return "", nil
	}
	return key, items
}

// propFields lists an object schema's properties as fields, in name
// order (a schema's map has no order), at most max.
func propFields(schema map[string]any, max int) []ViewField {
	props, _ := schema["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	var out []ViewField
	for _, k := range names {
		if len(out) == max {
			break
		}
		p, _ := props[k].(map[string]any)
		out = append(out, ViewField{ID: k, Label: humanize(k), Kind: schemaKind(p)})
	}
	return out
}

func schemaKind(p map[string]any) string {
	switch firstType(p["type"]) {
	case "integer", "number":
		return FieldNum
	case "boolean":
		return FieldBool
	case "object", "array":
		return FieldJSON
	case "string":
		switch str(p["format"]) {
		case "date-time", "date":
			return FieldTime
		case "uri", "url":
			return FieldLink
		}
	}
	return FieldText
}

// ── Shaping a result ───────────────────────────────────────────────────

// Cell is one typed value: S is always the shown text; N, T (Unix ms)
// and B carry the number, time and bool for typed filter and sort.
type Cell struct {
	S string  `json:"s"`
	N float64 `json:"n,omitempty"`
	T int64   `json:"t,omitempty"`
	B bool    `json:"b,omitempty"`
}

// ViewData is a shaped result: the layout, its fields and rows (a card
// or detail is one row), or text; Raw is the redacted JSON (or text) of
// the whole result, always set, so the phone opens it in one tap.
type ViewData struct {
	Layout    string      `json:"layout"`
	Fields    []ViewField `json:"fields,omitempty"`
	Rows      [][]Cell    `json:"rows,omitempty"`
	Text      string      `json:"text,omitempty"`
	Raw       string      `json:"raw"`
	Total     int         `json:"total,omitempty"`     // rows before the cap
	Truncated bool        `json:"truncated,omitempty"` // rows, text or raw were cut
	Error     bool        `json:"error,omitempty"`     // the tool said isError
}

// ShapeResult shapes a tool result for its view: fields as the view
// knows them (nil: from the data, in first-seen key order); compact
// turns a table into a list. Every string is redacted on the laptop.
func ShapeResult(res CallResult, fields []ViewField, compact bool) ViewData {
	payload, text := resultPayload(res)
	var d ViewData
	switch {
	case res.IsError:
		d = ViewData{Layout: LayoutStatus, Error: true, Text: clipText(redact(text), MaxViewText)}
	case payload != nil:
		d = ShapeJSON(payload, fields, compact)
	default:
		d = ViewData{Layout: LayoutText, Text: clipText(redact(text), MaxViewText)}
		d.Truncated = len(d.Text) < len(text)
	}
	d.Raw, d.Truncated = rawOf(res, payload, text, d.Truncated)
	return d
}

// resultPayload is the result's JSON: structuredContent, else the text
// content when it parses as JSON; text is the text content.
func resultPayload(res CallResult) (json.RawMessage, string) {
	var sb strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(c.Text)
		}
	}
	text := sb.String()
	if len(bytes.TrimSpace(res.StructuredContent)) > 0 && json.Valid(res.StructuredContent) {
		return res.StructuredContent, text
	}
	t := strings.TrimSpace(text)
	if (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t)) {
		return json.RawMessage(t), text
	}
	return nil, text
}

func rawOf(res CallResult, payload json.RawMessage, text string, cut bool) (string, bool) {
	var raw string
	switch {
	case payload != nil:
		var b bytes.Buffer
		if json.Indent(&b, payload, "", "  ") == nil {
			raw = b.String()
		} else {
			raw = string(payload)
		}
	case text != "":
		raw = text
	default:
		b, _ := json.MarshalIndent(res, "", "  ")
		raw = string(b)
	}
	raw = redact(raw)
	if len(raw) > MaxViewJSON {
		return clipText(raw, MaxViewJSON), true
	}
	return raw, cut
}

// ShapeJSON shapes one JSON value: an array of objects → table (list on
// compact); an object holding one array of objects → table over it; an
// array of scalars → list; one object → card (≤ 6 fields) or detail;
// anything else → json.
func ShapeJSON(raw json.RawMessage, fields []ViewField, compact bool) ViewData {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&v) != nil {
		return ViewData{Layout: LayoutJSON}
	}
	rows := raw
	if obj, ok := v.(map[string]any); ok {
		if k := wrappedArray(obj); k != "" {
			v = obj[k]
			rows = subRaw(raw, k)
		}
	}
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return ViewData{Layout: tableOrList(compact), Fields: fields}
		}
		if _, isObj := x[0].(map[string]any); isObj {
			if len(fields) == 0 {
				fields = capFields(learnFrom(rows), MaxTableFields)
			}
			d := ViewData{Layout: tableOrList(compact), Fields: fields, Total: len(x)}
			for i, e := range x {
				if i == MaxViewRows {
					d.Truncated = true
					break
				}
				o, _ := e.(map[string]any)
				d.Rows = append(d.Rows, rowOf(o, fields))
			}
			return d
		}
		d := ViewData{Layout: LayoutList, Fields: []ViewField{{ID: "value", Label: "Value", Kind: FieldText}}, Total: len(x)}
		for i, e := range x {
			if i == MaxViewRows {
				d.Truncated = true
				break
			}
			d.Rows = append(d.Rows, []Cell{cellOf(e)})
		}
		return d
	case map[string]any:
		if len(fields) == 0 {
			fields = capFields(learnFrom(rows), MaxViewFields)
		}
		layout := LayoutDetail
		if len(fields) <= cardMaxFields {
			layout = LayoutCard
		}
		return ViewData{Layout: layout, Fields: fields, Rows: [][]Cell{rowOf(x, fields)}, Total: 1}
	}
	return ViewData{Layout: LayoutJSON}
}

func tableOrList(compact bool) string {
	if compact {
		return LayoutList
	}
	return LayoutTable
}

// wrappedArray is the key of an object's only array of objects ("" when
// there is none or more than one).
func wrappedArray(obj map[string]any) string {
	key, n := "", 0
	for k, v := range obj {
		if a, ok := v.([]any); ok && len(a) > 0 {
			if _, isObj := a[0].(map[string]any); isObj {
				key, n = k, n+1
			}
		}
	}
	if n != 1 {
		return ""
	}
	return key
}

// subRaw is the raw JSON of obj[key] (to keep its keys' order).
func subRaw(raw json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m[key]
}

func rowOf(o map[string]any, fields []ViewField) []Cell {
	row := make([]Cell, len(fields))
	for i, f := range fields {
		v, ok := o[f.ID]
		if !ok {
			continue
		}
		c := cellOf(v)
		if f.Kind == FieldTime {
			if s, ok := v.(string); ok {
				if t, ok := parseTime(s); ok {
					c.T = t
				}
			}
		}
		row[i] = c
	}
	return row
}

// cellOf types one value; a nested object or list is a short value.
func cellOf(v any) Cell {
	switch x := v.(type) {
	case nil:
		return Cell{}
	case string:
		c := Cell{S: clipRunes(redact(x), MaxViewCell)}
		if t, ok := parseTime(x); ok {
			c.T = t
		}
		return c
	case json.Number:
		f, _ := x.Float64()
		return Cell{S: x.String(), N: f}
	case float64:
		return Cell{S: strconv.FormatFloat(x, 'f', -1, 64), N: x}
	case bool:
		if x {
			return Cell{S: "yes", B: true}
		}
		return Cell{S: "no"}
	case []any:
		if len(x) == 1 {
			return Cell{S: "[1 item]"}
		}
		return Cell{S: fmt.Sprintf("[%d items]", len(x))}
	case map[string]any:
		return Cell{S: "{…}"}
	}
	return Cell{S: clipRunes(redact(fmt.Sprint(v)), MaxViewCell)}
}

func parseTime(s string) (int64, bool) {
	if len(s) < 10 || len(s) > 40 || s[4] != '-' {
		return 0, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli(), true
		}
	}
	return 0, false
}

// ── Learning fields ────────────────────────────────────────────────────

// Learned is what a tool's first structured result taught its view:
// the fields and the layout of its shape.
type Learned struct {
	Fields []ViewField `json:"fields"`
	Layout string      `json:"layout"`
}

// LearnFields reads the fields of a structured result: the keys of the
// first object (of a list, of a wrapped list, or the object itself) in
// first-seen order, kinds by value type. ok false when the result has
// no object.
func LearnFields(res CallResult) (Learned, bool) {
	if res.IsError {
		return Learned{}, false
	}
	payload, _ := resultPayload(res)
	if payload == nil {
		return Learned{}, false
	}
	d := ShapeJSON(payload, nil, false)
	if len(d.Fields) == 0 || d.Layout == LayoutList && len(d.Fields) == 1 && d.Fields[0].ID == "value" {
		return Learned{}, false
	}
	return Learned{Fields: d.Fields, Layout: d.Layout}, true
}

// learnFrom reads the first object's keys in order, with kinds.
func learnFrom(raw json.RawMessage) []ViewField {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '[' {
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil || len(arr) == 0 {
			return nil
		}
		raw = bytes.TrimSpace(arr[0])
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var out []ViewField
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return out
		}
		key, _ := t.(string)
		var v any
		if dec.Decode(&v) != nil {
			return out
		}
		if key == "" || seen[key] || !utf8.ValidString(key) {
			continue
		}
		seen[key] = true
		out = append(out, ViewField{ID: key, Label: humanize(key), Kind: valueKind(v)})
	}
	return out
}

func valueKind(v any) string {
	switch x := v.(type) {
	case json.Number, float64:
		return FieldNum
	case bool:
		return FieldBool
	case []any, map[string]any:
		return FieldJSON
	case string:
		if _, ok := parseTime(x); ok {
			return FieldTime
		}
		if strings.HasPrefix(x, "https://") || strings.HasPrefix(x, "http://") {
			return FieldLink
		}
	}
	return FieldText
}

func capFields(f []ViewField, max int) []ViewField {
	if len(f) > max {
		return append([]ViewField(nil), f[:max]...)
	}
	return f
}

// ── Resources ──────────────────────────────────────────────────────────

// ResourceView is a server's resource view (mcp.<server>.resource): one
// param uri, picked from the resource list and the templates with their
// variables. Reading a resource never changes anything, so it runs from
// the phone unless the server is off.
func ResourceView(s Server, res []Resource, tmpl []ResourceTemplate) ToolView {
	v := ToolView{ID: ToolViewID(s.Name, ResourceViewTool), Server: s.Name, Tool: ResourceViewTool,
		Title: s.Name + " resources", Layout: LayoutText, Run: !s.Disabled,
		Params: []ViewParam{{ID: "uri", Label: "Resource", Kind: "uri", Required: true}}}
	if s.Disabled {
		v.Why = "the server is turned off"
	}
	for _, r := range res {
		if len(v.Resources) == maxResourceChoice {
			break
		}
		name := r.Name
		if r.Title != "" {
			name = r.Title
		}
		v.Resources = append(v.Resources, ResourceChoice{URI: r.URI, Name: oneLine(name, 80)})
	}
	for _, t := range tmpl {
		if len(v.Resources) == maxResourceChoice {
			break
		}
		name := t.Name
		if t.Title != "" {
			name = t.Title
		}
		v.Resources = append(v.Resources, ResourceChoice{URI: t.URITemplate, Name: oneLine(name, 80), Template: true, Vars: templateVars(t.URITemplate)})
	}
	return v
}

// templateVars lists a URI template's {vars}, in order, once each.
func templateVars(tmpl string) []string {
	var out []string
	seen := map[string]bool{}
	for {
		i := strings.IndexByte(tmpl, '{')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(tmpl[i:], '}')
		if j < 0 {
			return out
		}
		name := strings.TrimLeft(tmpl[i+1:i+j], "+#./;?&")
		for _, n := range strings.Split(name, ",") {
			n = strings.TrimSuffix(strings.TrimSpace(n), "*")
			if n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
		tmpl = tmpl[i+j+1:]
	}
}

// ShapeResource shapes a read resource: JSON array → table, JSON object
// → detail, markdown or plain text → text, binary → one line.
func ShapeResource(contents []ResourceContents, compact bool) ViewData {
	if len(contents) == 0 {
		return ViewData{Layout: LayoutText, Text: "(empty)", Raw: ""}
	}
	c := contents[0]
	if c.Text == "" && c.Blob != "" {
		line := fmt.Sprintf("binary %s, about %d bytes: open it on the laptop", orDefault(c.MimeType, "data"), len(c.Blob)*3/4)
		return ViewData{Layout: LayoutText, Text: line, Raw: line}
	}
	text := c.Text
	t := strings.TrimSpace(text)
	if strings.Contains(c.MimeType, "json") || (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t)) {
		if json.Valid([]byte(t)) {
			d := ShapeJSON(json.RawMessage(t), nil, compact)
			if d.Layout == LayoutCard {
				d.Layout = LayoutDetail // a resource object reads as a detail
			}
			d.Raw, d.Truncated = rawOf(CallResult{}, json.RawMessage(t), "", d.Truncated)
			return d
		}
	}
	d := ViewData{Layout: LayoutText, Text: clipText(redact(text), MaxViewText)}
	d.Truncated = len(d.Text) < len(text)
	d.Raw = d.Text
	return d
}

// ── Running from the phone ─────────────────────────────────────────────

// ViewArgs turns the phone's param values into tool arguments, typed by
// each param's kind. It refuses an unknown param, a missing required
// one, a value of the wrong kind, and a secret-looking value (type it on
// the laptop).
func ViewArgs(v ToolView, in map[string]string) (map[string]any, error) {
	byID := make(map[string]ViewParam, len(v.Params))
	for _, p := range v.Params {
		byID[p.ID] = p
	}
	out := make(map[string]any, len(in))
	for k, s := range in {
		p, ok := byID[k]
		if !ok {
			return nil, fmt.Errorf("unknown param %q (params: %s)", k, paramIDs(v.Params))
		}
		if ai.RedactSecrets(s) != s || ai.Redact(s) != s {
			return nil, fmt.Errorf("%s looks like a secret: type it on the laptop", p.Label)
		}
		if s == "" {
			continue
		}
		val, err := typedArg(p, s)
		if err != nil {
			return nil, err
		}
		out[k] = val
	}
	for _, p := range v.Params {
		if _, ok := out[p.ID]; p.Required && !ok {
			return nil, fmt.Errorf("%s is needed", p.Label)
		}
	}
	return out, nil
}

func typedArg(p ViewParam, s string) (any, error) {
	switch p.Kind {
	case "int":
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: a whole number", p.Label)
		}
		return n, nil
	case "number":
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: a number", p.Label)
		}
		return f, nil
	case "bool":
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("%s: true or false", p.Label)
		}
		return b, nil
	case "enum":
		for _, e := range p.Enum {
			if e == s {
				return s, nil
			}
		}
		return nil, fmt.Errorf("%s: one of %s", p.Label, strings.Join(p.Enum, ", "))
	}
	return s, nil
}

func paramIDs(ps []ViewParam) string {
	if len(ps) == 0 {
		return "none"
	}
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	return strings.Join(ids, ", ")
}

// ── Small helpers ──────────────────────────────────────────────────────

func redact(s string) string { return ai.Redact(ai.RedactSecrets(s)) }

func str(v any) string { s, _ := v.(string); return s }

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// humanize makes a label of an id: "created_at" → "Created at".
func humanize(id string) string {
	s := strings.NewReplacer("_", " ", "-", " ").Replace(id)
	var b strings.Builder
	prevLower := false
	for _, r := range s {
		if r >= 'A' && r <= 'Z' && prevLower {
			b.WriteByte(' ')
			r += 'a' - 'A'
		}
		prevLower = r >= 'a' && r <= 'z'
		b.WriteRune(r)
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if out == "" {
		return id
	}
	return strings.ToUpper(out[:1]) + out[1:]
}

// oneLine is s on one line, at most n runes.
func oneLine(s string, n int) string {
	return clipRunes(strings.Join(strings.Fields(s), " "), n)
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// clipText cuts s to at most n bytes on a rune boundary.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := n
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}
