package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/keymap"
)

// The buffer refresh (the notify the Lua sends): prompt names and
// script-set names are known, the lens shows the send key and the
// environment, completion gets the env names; the lens click sends, the
// cancel notify says nothing runs.
func TestCovHTTPBufferRefreshAndNotify(t *testing.T) {
	m, root, _ := covHTTP(t)
	m.keys = keymap.Default()
	if err := os.WriteFile(filepath.Join(root, httpclient.EnvFile), []byte(`{"dev":{"host":"http://127.0.0.1:1","region":"eu"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := httpSetSelectedEnv(httpRootFor(root), "dev"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "b.http")
	src := "###\n# @name a\n# @prompt user\n# @post set seen = 1\nGET {{host}}/u/{{user}}?s={{seen}}&r={{region}}&x={{nope}}&y={{open\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	covHTTPEdit(t, m, file, 5)
	var buf int
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_get_current_buf()`, &buf)
	if c := notifyHandlers["termocode_http_buf"](m, []any{int64(buf)}); c == nil {
		t.Error("the first refresh did not check the private env file")
	}
	// The row table: only {{nope}} is unknown.
	c, _, ok := m.httpBufParsed(buf)
	if !ok {
		t.Fatal("not parsed")
	}
	rv := httpResolver(file, c.f.Vars, "")
	if !rv.Known("region") || rv.Known("nope") {
		t.Error("env names")
	}
	lens := strings.Join(func() []string {
		var out []string
		for _, ch := range m.httpLensChunks(file, "a", "dev", false) {
			out = append(out, ch[0])
		}
		return out
	}(), "")
	if !strings.Contains(lens, "send Ctrl+Alt+R") || !strings.Contains(lens, "env dev") {
		t.Errorf("lens %q", lens)
	}
	// The cached parse is reused while the buffer does not change.
	if c2, _, _ := m.httpBufParsed(buf); c2 != c {
		t.Error("cache not reused")
	}
	// A stale cache entry (other buffer, same tick) is not used.
	m.httpS().bufCache = &httpBufCache{buf: buf + 1000, tick: c.tick}
	if c3, _, ok := m.httpBufParsed(buf); !ok || c3 == nil {
		t.Error("a fresh parse failed")
	}
	// Lens click: send the request at that line (it asks for user).
	m.promptOpen = false
	notifyHandlers["termocode_http_lens"](m, []any{int64(buf), int64(5)})
	if !m.promptOpen || m.promptKind != promptKindHTTPPromptVar {
		t.Errorf("lens click: prompt %v kind %v", m.promptOpen, m.promptKind)
	}
	covHTTPReset(m)
	notifyHandlers["termocode_http_cancel"](m, nil)
	if !strings.Contains(covHTTPToast(m), "No request is running") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// Not an .http buffer: no refresh, no check.
	_ = m.nvim.ExecLua(`vim.cmd('enew')`)
	var other int
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_get_current_buf()`, &other)
	if c := notifyHandlers["termocode_http_buf"](m, []any{int64(other)}); c != nil {
		t.Error("a scratch buffer was refreshed")
	}
	covHTTPReset(m)
	m.httpSendBufferLine(other, 1)
	if !strings.Contains(covHTTPToast(m), "not an .http file") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// httpBufText: the current buffer, a gone one.
	if _, lines, ok := m.httpBufText(buf); !ok || !strings.Contains(strings.Join(lines, "\n"), "{{nope}}") {
		t.Error("buffer text")
	}
	if _, _, ok := m.httpBufText(99999); ok {
		t.Error("a gone buffer has text")
	}
	if m.httpRefreshBuf(99999) != "" {
		t.Error("a gone buffer refreshed")
	}
	// Without an editor nothing is read or drawn.
	m.nvim = nil
	if _, _, ok := m.httpBufText(0); ok {
		t.Error("text without an editor")
	}
	if _, _, ok := m.httpBufParsed(0); ok {
		t.Error("parsed without an editor")
	}
	if m.httpRefreshBuf(0) != "" {
		t.Error("refreshed without an editor")
	}
	m.httpRefreshLens(file)
	m.ensureHTTPLua()
	if _, _, _, err := m.httpRequestFromBuffer(0, 0); err == nil || err.Error() != "no editor" {
		t.Errorf("err %v", err)
	}
	// No key bound to send: the lens has no key.
	m.keys = keymap.KeyMap{}
	if m.httpSendKeyLabel() != "" {
		t.Error("a key label without a binding")
	}
}
