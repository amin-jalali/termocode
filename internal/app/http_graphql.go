package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// GraphQL in .http files (API-18): fetch the schema of the request at
// the cursor (an introspection POST with the request's own URL and
// headers), complete a field at the cursor, check the query against the
// schema and show a type as SDL. Schemas are kept per URL for the
// session; a command that needs one fetches it first.

const (
	pickerKindHTTPGQLComplete pickerKindEnum = 5180 + iota
	pickerKindHTTPGQLDocs
)

// httpGQLSchemaMsg is the answer of an introspection fetch.
type httpGQLSchemaMsg struct {
	url    string
	schema *httpclient.GQLSchema
	err    error
	then   string // palette id to run once the schema is in ("" = none)
}

func httpGQLPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "http-gql-schema", Title: "HTTP: Fetch GraphQL Schema"},
		{ID: "http-gql-complete", Title: "HTTP: Complete GraphQL Field…"},
		{ID: "http-gql-validate", Title: "HTTP: Validate GraphQL Query"},
		{ID: "http-gql-docs", Title: "HTTP: GraphQL Schema Docs…"},
	}
}

// isGQLRequest: method GRAPHQL or the X-Request-Type: GraphQL header.
func isGQLRequest(r httpclient.Request) bool {
	return strings.EqualFold(r.Method, "GRAPHQL") || strings.EqualFold(r.Header("X-Request-Type"), "GraphQL")
}

// httpGQLBodyBefore is the body part of block text that runs from the
// request line to the cursor: everything after the first blank line
// (the request line and headers come before it). "" while the cursor is
// still in the headers.
func httpGQLBodyBefore(block string) string {
	block = strings.ReplaceAll(block, "\r\n", "\n")
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return ""
}

// httpGQLQuery is the query part of a GraphQL body: the JSON variables
// object after the last blank line is left out (as the send does).
func httpGQLQuery(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if i := strings.LastIndex(body, "\n\n"); i >= 0 {
		if tail := strings.TrimSpace(body[i:]); strings.HasPrefix(tail, "{") && strings.TrimSpace(body[:i]) != "" {
			return strings.TrimSpace(body[:i])
		}
	}
	return strings.TrimSpace(body)
}

// httpGQLPrefix is the name being typed at the end of src.
func httpGQLPrefix(src string) string {
	k := len(src)
	for k > 0 {
		c := src[k-1]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			k--
			continue
		}
		break
	}
	return src[k:]
}

// httpGQLIntrospection is rr turned into the introspection POST: same
// URL and headers, the introspection JSON as body.
func httpGQLIntrospection(rr httpclient.Resolved) httpclient.Resolved {
	out := rr
	out.Method = "POST"
	body := httpclient.GQLIntrospectionBody()
	out.Body = &body
	hs := make([]httpclient.Header, 0, len(rr.Headers)+1)
	for _, h := range rr.Headers {
		if strings.EqualFold(h.Name, "X-Request-Type") || strings.EqualFold(h.Name, "Content-Type") || strings.EqualFold(h.Name, "Content-Length") {
			continue
		}
		hs = append(hs, h)
	}
	out.Headers = append(hs, httpclient.Header{Name: "Content-Type", Value: "application/json"})
	return out
}

// httpGQLAtCursor is the GraphQL request at the cursor, resolved.
// warn != nil: a toast saying why there is none.
func (m *Model) httpGQLAtCursor() (req httpclient.Request, rr httpclient.Resolved, file string, warn tea.Cmd) {
	if m.httpCurrentFile() == "" {
		return req, rr, "", m.pushToastDetail(toast.Info, "Open an .http file first", "Put the cursor in a GRAPHQL request")
	}
	file, pf, r, err := m.httpRequestFromBuffer(0, 0)
	if err != nil {
		return req, rr, "", m.pushToastDetail(toast.Warn, "HTTP: "+err.Error(), "")
	}
	if !isGQLRequest(*r) {
		return req, rr, "", m.pushToastDetail(toast.Info, "The request at the cursor is not GraphQL",
			"Use GRAPHQL as the method, or add the header X-Request-Type: GraphQL")
	}
	rv := httpResolver(file, pf.Vars, "")
	rv.Responses = m.httpS().chainSource(file)
	rr, _ = rv.Resolve(*r)
	return *r, rr, file, nil
}

// dispatchHTTPGQL runs an "http-gql-…" palette id. ok=false: not ours.
func (m *Model) dispatchHTTPGQL(id string) (tea.Cmd, bool) {
	switch id {
	case "http-gql-schema", "http-gql-complete", "http-gql-validate", "http-gql-docs":
	default:
		return nil, false
	}
	req, rr, file, warn := m.httpGQLAtCursor()
	if warn != nil {
		return warn, true
	}
	s := m.httpS()
	sc := s.gqlSchemas[rr.URL]
	if id == "http-gql-schema" || sc == nil {
		then := id
		if id == "http-gql-schema" {
			then = ""
		}
		return tea.Batch(m.pushToastDetail(toast.Info, "Fetching the GraphQL schema…", rr.URL), m.httpGQLFetch(rr, file, then)), true
	}
	switch id {
	case "http-gql-complete":
		return m.openHTTPGQLComplete(req, sc), true
	case "http-gql-validate":
		return m.httpGQLValidate(req, sc), true
	}
	return m.openHTTPGQLDocs(sc), true
}

// httpGQLFetch sends the introspection off the UI loop.
func (m *Model) httpGQLFetch(rr httpclient.Resolved, file, then string) tea.Cmd {
	s := m.httpS()
	set := loadHTTPSettings()
	client := s.httpClient(set)
	in := httpGQLIntrospection(rr)
	var hooks httpclient.Hooks
	if set.tlsInsecure() {
		hooks.Insecure = true
	}
	httpUserTLS(&httpRun{rr: in, file: file}, &hooks)
	if u := httpclient.HostKey(in.URL); u != "" && s.insecure[u] {
		hooks.Insecure = true
	}
	key := rr.URL
	timeout := set.timeout()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		resp := client.Send(ctx, in, hooks)
		msg := httpGQLSchemaMsg{url: key, then: then}
		switch {
		case resp == nil:
			msg.err = fmt.Errorf("no answer")
		case resp.Err != "":
			msg.err = fmt.Errorf("%s", resp.Err)
		case resp.Status >= 400:
			msg.err = fmt.Errorf("the server answered %d %s", resp.Status, resp.StatusText)
		default:
			msg.schema, msg.err = httpclient.ParseGQLSchema(resp.Body)
		}
		return msg
	}
}

// onHTTPGQLSchema stores a fetched schema and runs the waiting command.
func (m *Model) onHTTPGQLSchema(msg httpGQLSchemaMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Could not fetch the GraphQL schema", msg.err.Error())
	}
	s := m.httpS()
	if s.gqlSchemas == nil {
		s.gqlSchemas = map[string]*httpclient.GQLSchema{}
	}
	s.gqlSchemas[msg.url] = msg.schema
	if msg.then != "" {
		cmd, _ := m.dispatchHTTPGQL(msg.then)
		return cmd
	}
	return m.pushToastDetail(toast.Info, fmt.Sprintf("GraphQL schema: %d types", len(msg.schema.Types)),
		"HTTP: Complete GraphQL Field…, Validate GraphQL Query and GraphQL Schema Docs… use it")
}

// httpGQLTextBefore is the request block from the request line to the
// cursor (the char under a normal-mode cursor counts as typed).
func (m *Model) httpGQLTextBefore(req httpclient.Request) string {
	if m.nvim == nil {
		return ""
	}
	var text string
	_ = m.nvim.ExecLuaResult(`local first = ...
local c = vim.api.nvim_win_get_cursor(0)
if c[1] < first then return '' end
local lines = vim.api.nvim_buf_get_lines(0, first - 1, c[1], false)
local off = (vim.fn.mode() == 'i') and 0 or 1
lines[#lines] = string.sub(lines[#lines], 1, c[2] + off)
return table.concat(lines, '\n')`, &text, req.Line)
	return text
}

// openHTTPGQLComplete lists the fields that fit at the cursor.
func (m *Model) openHTTPGQLComplete(req httpclient.Request, sc *httpclient.GQLSchema) tea.Cmd {
	src := httpGQLBodyBefore(m.httpGQLTextBefore(req))
	fields := sc.Complete(src)
	if len(fields) == 0 {
		return m.pushToastDetail(toast.Info, "No GraphQL field fits here", "Put the cursor inside a { } selection")
	}
	s := m.httpS()
	s.gqlPrefix = httpGQLPrefix(src)
	items := make([]picker.Item, 0, len(fields))
	for _, f := range fields {
		hint := f.Type
		if f.Description != "" {
			hint += " · " + f.Description
		}
		items = append(items, picker.Item{ID: f.Name, Title: f.Name, Hint: hint})
	}
	return m.openHTTPPicker(pickerKindHTTPGQLComplete, " GraphQL Field ", items, nil)
}

// httpGQLInsert types the rest of name at the cursor.
func (m *Model) httpGQLInsert(name string) tea.Cmd {
	rest := strings.TrimPrefix(name, m.httpS().gqlPrefix)
	if m.nvim == nil || rest == "" {
		return nil
	}
	_ = m.nvim.ExecLuaArgs(`local s = ...
local c = vim.api.nvim_win_get_cursor(0)
local line = vim.api.nvim_get_current_line()
local col = c[2]
if vim.fn.mode() ~= 'i' and #line > 0 then col = col + 1 end
if col > #line then col = #line end
vim.api.nvim_buf_set_text(0, c[1] - 1, col, c[1] - 1, col, { s })
vim.api.nvim_win_set_cursor(0, { c[1], col + #s - 1 })`, rest)
	m.focus = FocusEditor
	return nil
}

// httpGQLValidate checks the query's fields and braces.
func (m *Model) httpGQLValidate(req httpclient.Request, sc *httpclient.GQLSchema) tea.Cmd {
	body := ""
	if req.Body != nil {
		body = *req.Body
	}
	errs := sc.Validate(httpGQLQuery(body))
	if len(errs) == 0 {
		return m.pushToast(toast.Info, "GraphQL query OK")
	}
	title := "1 GraphQL problem"
	if len(errs) > 1 {
		title = fmt.Sprintf("%d GraphQL problems", len(errs))
	}
	return m.pushToastDetail(toast.Warn, title, strings.Join(errs, "; "))
}

// openHTTPGQLDocs lists the schema's types; one opens as SDL.
func (m *Model) openHTTPGQLDocs(sc *httpclient.GQLSchema) tea.Cmd {
	m.httpS().gqlDocs = sc
	names := make([]string, 0, len(sc.Types))
	for n := range sc.Types {
		if !strings.HasPrefix(n, "__") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	root := map[string]bool{sc.Query: true, sc.Mutation: true, sc.Subscription: true}
	items := make([]picker.Item, 0, len(names))
	for _, n := range names {
		if root[n] && n != "" {
			items = append(items, picker.Item{ID: n, Title: n, Hint: "root"})
		}
	}
	for _, n := range names {
		if !root[n] {
			items = append(items, picker.Item{ID: n, Title: n, Hint: strings.ToLower(sc.Types[n].Kind)})
		}
	}
	return m.openHTTPPicker(pickerKindHTTPGQLDocs, " GraphQL Schema Docs ", items, nil)
}

// httpGQLPickerSelect handles the two GraphQL pickers. ok=false: not ours.
func (m *Model) httpGQLPickerSelect(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindHTTPGQLComplete:
		return m.httpGQLInsert(id), true
	case pickerKindHTTPGQLDocs:
		sc := m.httpS().gqlDocs
		if sc == nil {
			return nil, true
		}
		m.ensureEditorWindowCurrent()
		if err := m.openReadOnlyPage("GraphQL: "+id, sc.Docs(id), "", "graphql"); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not open the GraphQL docs", err.Error()), true
		}
		m.focus = FocusEditor
		return nil, true
	}
	return nil, false
}
