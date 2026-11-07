package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// HTTP: Edit Params and Headers… (API-3): the request at the cursor as
// a table, one row per query param and header. Enter on a row edits it
// in a one-line prompt ("name=value" / "name: value"; empty text
// removes it). Each edit rewrites only the request line and the header
// lines in the buffer, and the table is read again from the text, so
// the two always agree. Undo in the editor undoes an edit.

const pickerKindHTTPFields pickerKindEnum = 3833

const promptKindHTTPField promptKindEnum = 3833

// httpFieldsState is the buffer and request the table shows, and the
// row the open prompt edits.
type httpFieldsState struct {
	buf, line int
	kind      httpclient.FieldKind
	index     int
}

// httpFieldText is a row as the prompt shows it.
func httpFieldText(f httpclient.Field) string {
	if f.Kind == httpclient.FieldHeader {
		return f.Name + ": " + f.Value
	}
	if f.HasValue {
		return f.Name + "=" + f.Value
	}
	return f.Name
}

// httpFieldItems is the table: params, headers, then the two add rows.
// Row ids are "p<i>" / "h<i>" (the index inside its kind; -1 = add).
func httpFieldItems(fields []httpclient.Field) []picker.Item {
	var items []picker.Item
	np, nh := 0, 0
	for _, f := range fields {
		if f.Kind == httpclient.FieldParam {
			title := f.Name
			if f.HasValue {
				title += " = " + f.Value
			}
			items = append(items, picker.Item{ID: fmt.Sprintf("p%d", np), Title: title, Hint: "query param"})
			np++
		}
	}
	for _, f := range fields {
		if f.Kind == httpclient.FieldHeader {
			items = append(items, picker.Item{ID: fmt.Sprintf("h%d", nh), Title: httpFieldText(f), Hint: "header"})
			nh++
		}
	}
	return append(items,
		picker.Item{ID: "p-1", Title: "+ Add Query Param…"},
		picker.Item{ID: "h-1", Title: "+ Add Header…"})
}

// httpFieldRowID reads a row id of httpFieldItems.
func httpFieldRowID(id string) (httpclient.FieldKind, int, bool) {
	if len(id) < 2 {
		return 0, 0, false
	}
	var kind httpclient.FieldKind
	switch id[0] {
	case 'p':
		kind = httpclient.FieldParam
	case 'h':
		kind = httpclient.FieldHeader
	default:
		return 0, 0, false
	}
	var i int
	if _, err := fmt.Sscan(id[1:], &i); err != nil || i < -1 || fmt.Sprint(i) != id[1:] {
		return 0, 0, false
	}
	return kind, i, true
}

// httpFieldInput reads the prompt text into an edit of row index.
func httpFieldInput(kind httpclient.FieldKind, index int, s string) httpclient.FieldEdit {
	e := httpclient.FieldEdit{Kind: kind, Index: index}
	s = strings.TrimSpace(s)
	if s == "" {
		e.Delete = true
		return e
	}
	sep := "="
	if kind == httpclient.FieldHeader {
		sep = ":"
	}
	name, value, has := strings.Cut(s, sep)
	e.Name, e.Value, e.HasValue = strings.TrimSpace(name), strings.TrimSpace(value), has
	return e
}

// openHTTPFields opens the table for the request at the cursor (buf=0,
// line=0) or at line of buffer buf.
func (m *Model) openHTTPFields(buf, line int) tea.Cmd {
	c, cursor, ok := m.httpBufParsed(buf)
	if !ok {
		return m.pushToastDetail(toast.Warn, "HTTP: no editor", "")
	}
	if !isHTTPPath(c.name) {
		return m.pushToastDetail(toast.Info, "Open an .http file first", "HTTP: New .http File… makes one")
	}
	if line <= 0 {
		line = cursor
	}
	r := c.f.AtLine(line)
	if r == nil {
		return m.pushToastDetail(toast.Info, "No request in this file", "")
	}
	m.httpS().fields = httpFieldsState{buf: c.buf, line: r.Line}
	note := []string{r.Method + " " + r.URL, "Enter edits a row; empty text removes it. The text follows each edit."}
	return m.openHTTPPicker(pickerKindHTTPFields, " Params and Headers ", httpFieldItems(r.Fields()), note)
}

// httpFieldsPick opens the prompt for a table row.
func (m *Model) httpFieldsPick(id string) tea.Cmd {
	kind, idx, ok := httpFieldRowID(id)
	if !ok {
		return nil
	}
	st := &m.httpS().fields
	st.kind, st.index = kind, idx
	c, _, ok := m.httpBufParsed(st.buf)
	if !ok {
		return nil
	}
	r := c.f.AtLine(st.line)
	if r == nil {
		return nil
	}
	initial, n := "", -1
	for _, f := range r.Fields() {
		if f.Kind != kind {
			continue
		}
		if n++; n == idx {
			initial = httpFieldText(f)
		}
	}
	title, label := "HTTP: Query Param", "name=value:"
	if kind == httpclient.FieldHeader {
		title, label = "HTTP: Header", "name: value:"
	}
	if idx == -1 {
		title = strings.Replace(title, "HTTP: ", "HTTP: Add ", 1)
	}
	return m.openHTTPPrompt(promptKindHTTPField, title, label, initial)
}

// httpFieldsApply writes the prompt's edit into the buffer and shows
// the table again, read from the new text.
func (m *Model) httpFieldsApply(value string) tea.Cmd {
	st := m.httpS().fields
	e := httpFieldInput(st.kind, st.index, value)
	if e.Delete && e.Index == -1 {
		return m.openHTTPFields(st.buf, st.line)
	}
	c, _, ok := m.httpBufParsed(st.buf)
	if !ok || m.nvim == nil {
		return m.pushToastDetail(toast.Warn, "HTTP: no editor", "")
	}
	r := c.f.AtLine(st.line)
	if r == nil {
		return m.pushToastDetail(toast.Warn, "HTTP: the request is gone", "")
	}
	start, end, repl, err := httpclient.EditField(c.lines, r, e)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "HTTP: "+err.Error(), "")
	}
	if err := m.nvim.ExecLuaArgs(`local b, s, e, lines = ...
vim.api.nvim_buf_set_lines(b, s, e, false, lines)`, c.buf, start, end, repl); err != nil {
		return m.pushToastDetail(toast.Errr, "HTTP: edit failed", err.Error())
	}
	return tea.Batch(m.openHTTPFields(c.buf, r.Line), m.fetchStateCmd())
}
