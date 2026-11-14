package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

func covHTTPBufText(t *testing.T, m *Model) string {
	t.Helper()
	var text string
	if err := m.nvim.ExecLuaResult(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &text); err != nil {
		t.Fatal(err)
	}
	return text
}

// The params and headers table edits the request text in the buffer:
// change a param, add a header, remove a param; the add rows with no
// text only reopen the table.
func TestCovHTTPFieldsEditBuffer(t *testing.T) {
	m, root, _ := covHTTP(t)
	file := filepath.Join(root, "f.http")
	if err := os.WriteFile(file, []byte("###\nGET http://x/a?q=1&flag\nAccept: json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covHTTPEdit(t, m, file, 2)
	covHTTPDispatch(t, m, "http-fields")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPFields {
		t.Fatalf("no table: %q", covHTTPToast(m))
	}
	got := covSvcPickerText(m)
	for _, want := range []string{"q = 1", "flag", "Accept: json", "+ Add Query Param", "+ Add Header"} {
		if !strings.Contains(got, want) {
			t.Errorf("table lacks %q:\n%s", want, got)
		}
	}
	pick := func(id string) {
		t.Helper()
		m.pickerOpen = false
		m.pickerKind = pickerKindHTTPFields
		m.handleHTTPPickerSelect(id)
	}
	pick("p0")
	if !m.promptOpen || m.promptKind != promptKindHTTPField || m.prompt.Value() != "q=1" {
		t.Fatalf("param prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.handleHTTPPromptSubmit("q=2")
	if got := covHTTPBufText(t, m); !strings.Contains(got, "GET http://x/a?q=2&flag") {
		t.Errorf("after param edit:\n%s", got)
	}
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPFields {
		t.Error("the table did not reopen")
	}
	pick("h0")
	if m.prompt.Value() != "Accept: json" || !strings.Contains(m.prompt.View(), "Header") {
		t.Errorf("header prompt %q", m.prompt.Value())
	}
	pick("h-1")
	if m.prompt.Value() != "" {
		t.Errorf("add prompt %q", m.prompt.Value())
	}
	m.handleHTTPPromptSubmit("X-A: b")
	if got := covHTTPBufText(t, m); !strings.Contains(got, "X-A: b") {
		t.Errorf("after add:\n%s", got)
	}
	pick("p1")
	m.handleHTTPPromptSubmit("")
	if got := covHTTPBufText(t, m); !strings.Contains(got, "GET http://x/a?q=2\n") {
		t.Errorf("after remove:\n%s", got)
	}
	pick("p-1")
	m.pickerOpen = false
	m.handleHTTPPromptSubmit(" ")
	if !m.pickerOpen {
		t.Error("an empty add did not reopen the table")
	}
	// Ids that are no row.
	for _, id := range []string{"z1", "p", "px", "p-2", "p01"} {
		if _, _, ok := httpFieldRowID(id); ok {
			t.Errorf("%q read as a row", id)
		}
		if m.httpFieldsPick(id) != nil {
			t.Errorf("%q picked", id)
		}
	}
	if httpFieldText(httpclient.Field{Kind: httpclient.FieldParam, Name: "bare"}) != "bare" {
		t.Error("a param with no value")
	}
	// An edit the request cannot take says why.
	st := &m.httpS().fields
	st.kind, st.index = httpclient.FieldParam, 7
	covHTTPReset(m)
	m.httpFieldsApply("a=b")
	if !strings.Contains(covHTTPToast(m), "HTTP:") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// The request went away from the buffer.
	_ = m.nvim.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, { '# nothing' })`)
	covHTTPReset(m)
	m.httpFieldsApply("a=b")
	if !strings.Contains(covHTTPToast(m), "the request is gone") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	if m.httpFieldsPick("p0") != nil {
		t.Error("picked in a buffer with no request")
	}
	covHTTPReset(m)
	m.openHTTPFields(0, 0)
	if !strings.Contains(covHTTPToast(m), "No request in this file") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// A buffer that is gone.
	st.buf = 99999
	if m.httpFieldsPick("p0") != nil {
		t.Error("picked in a gone buffer")
	}
	covHTTPReset(m)
	m.httpFieldsApply("a=b")
	if !strings.Contains(covHTTPToast(m), "no editor") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	covHTTPReset(m)
	m.openHTTPFields(99999, 1)
	if !strings.Contains(covHTTPToast(m), "no editor") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}
