package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"termocode/extensions"
	"termocode/internal/activity"
	"termocode/internal/editor"
	"termocode/internal/ext"
	"termocode/internal/nvim"
)

// ── helpers ──────────────────────────────────────────────────────────────

func writeExt(t *testing.T, root, name, initLua string, extra map[string]string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, body := range extra {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// extTestModel attaches a real nvim with every notify method subscribed and
// returns a Model plus a channel of the notifications Lua sends.
func extTestModel(t *testing.T) (*Model, <-chan nvim.NotifyMsg) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	m := &Model{w: 100, h: 30, nvim: c, ext: newExtState(), showExp: true, explorerWidth: 30,
		activity: activity.New(), output: newOutputStore(), editor: editor.New(c)}
	m.subscribeNotify()
	notes := make(chan nvim.NotifyMsg, 256)
	go func() {
		for {
			msg := c.Next()()
			if n, ok := msg.(nvim.NotifyMsg); ok {
				notes <- n
			}
			if msg == nil {
				return
			}
		}
	}()
	t.Cleanup(func() { activity.SetExtraItems(nil) })
	return m, notes
}

func waitExtNotify(t *testing.T, notes <-chan nvim.NotifyMsg, method string) nvim.NotifyMsg {
	t.Helper()
	return waitExtNotifyWith(t, notes, method, "")
}

// waitExtNotifyWith waits for method whose args mention substr.
func waitExtNotifyWith(t *testing.T, notes <-chan nvim.NotifyMsg, method, substr string) nvim.NotifyMsg {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case n := <-notes:
			if n.Method == method && strings.Contains(fmt.Sprint(n.Args...), substr) {
				return n
			}
		case <-deadline:
			t.Fatalf("no %s notification within 5s", method)
		}
	}
}

func loadExtensions(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.extLoadCmd(false)
	if cmd == nil {
		t.Fatal("extLoadCmd returned nil")
	}
	msg, ok := cmd().(extLoadedMsg)
	if !ok {
		t.Fatal("extLoadCmd did not return extLoadedMsg")
	}
	if msg.err != nil {
		t.Fatalf("load error: %v", msg.err)
	}
	m.onExtLoaded(msg)
	m.ext.polling = false // the poll Cmd onExtLoaded returned is not run here
}

// poll runs one heartbeat poll synchronously.
func poll(t *testing.T, m *Model) extPollMsg {
	t.Helper()
	m.ext.polling = false
	cmd := m.extPollCmd()
	if cmd == nil {
		t.Fatal("extPollCmd returned nil")
	}
	msg, _ := cmd().(extPollMsg)
	m.updateExt(msg)
	return msg
}

func evalLua(t *testing.T, m *Model, code string) string {
	t.Helper()
	out, err := m.nvim.EvalLuaString(code)
	if err != nil {
		t.Fatalf("lua %q: %v", code, err)
	}
	return out
}

// ── tests ────────────────────────────────────────────────────────────────

// TestExtensionHostNvim loads good and broken extensions into a real nvim:
// broken ones are reported but never stop the others, an endless loop is
// cut off by the time budget, and commands / panels / status items /
// prompt / pick work end to end.
func TestExtensionHostNvim(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	root := filepath.Join(cfg, "termocode", "extensions")
	writeExt(t, root, "good", `
local tc = termocode
local helper = require('good_helper')
tc.register_command({ id = 'hello', title = 'Say Hello', run = function() vim.g.good_ran = helper.value end })
tc.register_command({ id = 'ask', title = 'Ask', run = function()
  tc.prompt({ title = 'Q', label = 'Name:', default = 'x' }, function(v) vim.g.good_prompt = v or 'CANCELLED' end)
end })
tc.register_command({ id = 'choose', title = 'Choose', run = function()
  tc.pick({ title = 'Pick', items = { 'one', { title = 'two', hint = 'h', tag = 2 } } }, function(it)
    vim.g.good_pick = it and (it.tag or it.title) or 'NONE'
  end)
end })
tc.register_command({ id = 'oops', title = 'Oops', run = function() error('command blew up') end })
tc.register_panel({ id = 'main', title = 'Good', icon = 'G', render = function(w, h)
  return { '{{accent}}width ' .. w .. '{{/}}', 'row two', 'GET {{host}}' }
end, on_select = function(row, line) vim.g.good_selected = row .. ':' .. line end })
tc.register_status_item({ id = 'st', text = function() return '{{muted}}st ok{{/}}' end, command = 'hello' })
tc.on('ready', function() vim.g.good_ready = 1 end)
`, map[string]string{
		"lua/good_helper.lua": "return { value = 42 }",
		"extension.json":      `{"version":"2.0.0"}`,
	})
	writeExt(t, root, "broken", "this is not lua (", nil)
	writeExt(t, root, "boom", "error('kaboom')", nil)
	writeExt(t, root, "spin", "local x = 0 while true do x = x + 1 end", nil)
	writeExt(t, root, "slowpanel", `
termocode.register_panel({ id = 'p', title = 'Slow', render = function() while true do end end })
`, nil)

	m, notes := extTestModel(t)
	start := time.Now()
	loadExtensions(t, m)
	if d := time.Since(start); d > 8*time.Second {
		t.Fatalf("loading took %v", d)
	}
	waitExtNotify(t, notes, "termocode_ext_error") // at least one error reached Go

	got := map[string]ext.Loaded{}
	for _, x := range m.ext.reg.Extensions {
		got[x.Name] = x
	}
	want := map[string]bool{"good": true, "broken": false, "boom": false, "spin": false, "slowpanel": true}
	for name, ok := range want {
		if x, found := got[name]; !found || x.OK != ok {
			t.Errorf("extension %s: %+v (want ok=%v)", name, x, ok)
		}
	}
	if got["good"].Version != "2.0.0" {
		t.Errorf("version = %q", got["good"].Version)
	}
	log := evalLua(t, m, "return _G._termocode_ext.log_text()")
	if !strings.Contains(log, "timed out") || !strings.Contains(log, "kaboom") {
		t.Errorf("log misses timeout / error:\n%s", log)
	}
	if evalLua(t, m, "return tostring(vim.g.good_ready)") != "1" {
		t.Error("ready event did not fire")
	}

	// Palette rows.
	items := m.extPaletteItems()
	titles := map[string]string{}
	for _, it := range items {
		titles[it.ID] = it.Title
	}
	if titles["ext-good.hello"] != "Ext: Say Hello" {
		t.Errorf("palette items = %+v", items)
	}

	// Command through the palette dispatcher.
	cmd, ok := m.dispatchExtPalette("ext-good.hello")
	if !ok || cmd == nil {
		t.Fatal("palette dispatch did not run the command")
	}
	if done, _ := cmd().(extCallDoneMsg); done.err != nil {
		t.Fatal(done.err)
	}
	if evalLua(t, m, "return tostring(vim.g.good_ran)") != "42" {
		t.Error("command did not run (or module require failed)")
	}
	// A failing command is reported, not fatal.
	m.runExtCommand("good.oops")()
	if n := waitExtNotifyWith(t, notes, "termocode_ext_error", "command blew up"); notifyArgString(n.Args, 1) != "command good.oops" {
		t.Errorf("error notify = %#v", n.Args)
	}

	// Panels: activity item + poll + render.
	if len(m.ext.reg.Panels) != 2 {
		t.Fatalf("panels = %+v", m.ext.reg.Panels)
	}
	m.activity.SetActive(activity.ViewExtBase) // "Good"
	m.focus = FocusExplorer
	pm := poll(t, m)
	lines := m.ext.lines["good.main"]
	if len(lines) != 3 || !strings.Contains(lines[0], "width 29") {
		t.Fatalf("panel lines = %q (err %v)", lines, pm.err)
	}
	if st := m.extStatusTexts(); len(st) != 1 || st[0] != "{{muted}}st ok{{/}}" {
		t.Errorf("status = %q", st)
	}
	out := m.renderExtSidebar(29, 10)
	rows := strings.Split(out, "\n")
	if len(rows) != 10 {
		t.Fatalf("sidebar rows = %d", len(rows))
	}
	for i, r := range rows {
		if w := lipglossWidth(r); w != 29 {
			t.Errorf("row %d width %d", i, w)
		}
	}
	if !strings.Contains(stripANSIForTest(out), "GET {{host}}") {
		t.Error("unknown {{tag}} should render literally")
	}
	// Wired into the real sidebar: header + divider, exact width.
	for i, r := range strings.Split(m.renderSidebar(m.h), "\n") {
		if w := lipglossWidth(r); w != m.explorerWidth {
			t.Fatalf("renderSidebar row %d width %d, want %d", i, w, m.explorerWidth)
		}
	}
	// Status item click runs its command (good.hello sets good_ran = 42).
	_ = evalLua(t, m, "vim.g.good_ran = 0; return ''")
	m.status.SetWidth(m.w)
	var clicked bool
	for x := 0; x < m.w && !clicked; x++ {
		if c, ok := m.hitExtStatusItem(x, m.h-1); ok && c != nil {
			c()
			clicked = true
		}
	}
	if !clicked || evalLua(t, m, "return tostring(vim.g.good_ran)") != "42" {
		t.Errorf("status item click: clicked=%v", clicked)
	}

	// Select via keyboard: down, Enter → on_select(2, 'row two').
	m.handleExtSidebarKey(keyMsg("down"))
	c := m.handleExtSidebarKey(keyMsg("enter"))
	if c == nil {
		t.Fatal("enter gave no command")
	}
	c()
	if v := evalLua(t, m, "return vim.g.good_selected"); v != "2:row two" {
		t.Errorf("selected = %q", v)
	}
	// Mouse: click on row 3 (y=3: header is y=0).
	m.handleExtSidebarMouse(3, mouseLeft)()
	if v := evalLua(t, m, "return vim.g.good_selected"); v != "3:GET {{host}}" {
		t.Errorf("clicked = %q", v)
	}

	// A panel whose render never returns is cut off and marked failed.
	m.activity.SetActive(activity.ViewExtBase + 1)
	for i := 0; i < 4; i++ {
		poll(t, m)
	}
	if l := m.ext.lines["slowpanel.p"]; len(l) == 0 || !strings.Contains(l[0], "failed") {
		t.Errorf("slow panel lines = %q", l)
	}

	// prompt round trip.
	m.runExtCommand("good.ask")()
	n := waitExtNotify(t, notes, "termocode_ext_prompt")
	m.dispatchNotify(n)
	if !m.promptOpen || m.promptKind != promptKindExt || m.ext.pendingPrompt == 0 {
		t.Fatalf("prompt not opened: open=%v kind=%v req=%d", m.promptOpen, m.promptKind, m.ext.pendingPrompt)
	}
	m.extPromptSubmit("bob")()
	if v := evalLua(t, m, "return vim.g.good_prompt"); v != "bob" {
		t.Errorf("prompt value = %q", v)
	}
	// cancelled prompt → nil
	m.runExtCommand("good.ask")()
	m.dispatchNotify(waitExtNotify(t, notes, "termocode_ext_prompt"))
	m.extOverlayClosed()()
	if v := evalLua(t, m, "return vim.g.good_prompt"); v != "CANCELLED" {
		t.Errorf("cancelled prompt value = %q", v)
	}

	// pick round trip: item 2 is the table item.
	m.runExtCommand("good.choose")()
	m.dispatchNotify(waitExtNotify(t, notes, "termocode_ext_pick"))
	if !m.pickerOpen || m.pickerKind != pickerKindExt {
		t.Fatal("picker not opened")
	}
	m.extPickSelect("2")()
	if v := evalLua(t, m, "return tostring(vim.g.good_pick)"); v != "2" {
		t.Errorf("pick = %q", v)
	}

	// Reload drops everything and loads again.
	_ = m.extReload()
	if m.ext.loaded {
		t.Error("reload should mark the host unloaded until the load returns")
	}
	m.ext.loading = false // the load Cmd inside extReload's batch is not run here
	msg, _ := m.extLoadCmd(true)().(extLoadedMsg)
	m.onExtLoaded(msg)
	if len(m.ext.reg.Commands) != 4 {
		t.Errorf("after reload commands = %d, want 4 (no duplicates)", len(m.ext.reg.Commands))
	}
}

// TestSampleExtensionsLoadNvim installs the bundled samples and loads
// them: none may report an error, and the REST parser module works.
func TestSampleExtensionsLoadNvim(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	root := filepath.Join(cfg, "termocode", "extensions")
	if _, err := ext.InstallBundled(extensions.FS, root); err != nil {
		t.Fatal(err)
	}
	m, _ := extTestModel(t)
	loadExtensions(t, m)
	for _, x := range m.ext.reg.Extensions {
		if !x.OK {
			t.Errorf("sample %s failed: %s", x.Name, x.Error)
		}
	}
	if len(m.ext.reg.Extensions) != 4 {
		t.Errorf("loaded %d samples, want 4", len(m.ext.reg.Extensions))
	}
	ids := map[string]bool{}
	for _, c := range m.ext.reg.Commands {
		ids[c.ID] = true
	}
	for _, id := range []string{"todo-tree.refresh", "word-count.details", "open-in-github.open", "rest-client.send"} {
		if !ids[id] {
			t.Errorf("missing command %s (have %v)", id, ids)
		}
	}
	if log := evalLua(t, m, "return _G._termocode_ext.log_text()"); strings.Contains(log, "ERROR") {
		t.Errorf("sample log has errors:\n%s", log)
	}

	// Every panel renders without error.
	for i, p := range m.ext.reg.Panels {
		m.activity.SetActive(activity.ViewExtBase + activity.View(i))
		pm := poll(t, m)
		if pm.err != nil || len(pm.poll.Lines) == 0 || strings.Contains(strings.Join(pm.poll.Lines, "\n"), "render failed") {
			t.Errorf("panel %s: lines=%q err=%v", p.ID, pm.poll.Lines, pm.err)
		}
	}

	// REST parser (port of http_file_parser.dart).
	out := evalLua(t, m, `
local p = require('rest_client.parser')
local src = table.concat({
  '@host = https://api.example.com',
  '@token = abc',
  '',
  '### List items',
  '# @name list',
  'GET {{host}}/items HTTP/1.1',
  'Accept: application/json',
  'Authorization: Bearer {{token}}',
  '',
  '###',
  'POST {{host}}/items',
  'Content-Type: application/json',
  '',
  '{',
  '  "name": "{{missing}}"',
  '}',
  '',
  '',
  '### only comments',
  '# nothing here',
  '###',
  'https://example.com/plain',
}, '\n')
local reqs, vars = p.parse(src)
local r1 = p.resolve(reqs[1], vars)
local r2 = p.resolve(reqs[2], vars)
return vim.json.encode({
  n = #reqs,
  name1 = reqs[1].name, url1 = r1.url, auth = r1.headers.Authorization, start1 = reqs[1].start_line,
  m2 = r2.method, body2 = r2.body, ct = r2.headers['Content-Type'],
  m3 = reqs[3].method, url3 = reqs[3].url,
  at = p.at_line(reqs, 13).method,
  pretty = require('rest_client.format').pretty_json('{"a":[1,2],"b":{},"c":"x,y"}'),
})`)
	var r struct {
		N      int    `json:"n"`
		Name1  string `json:"name1"`
		URL1   string `json:"url1"`
		Auth   string `json:"auth"`
		Start1 int    `json:"start1"`
		M2     string `json:"m2"`
		Body2  string `json:"body2"`
		CT     string `json:"ct"`
		M3     string `json:"m3"`
		URL3   string `json:"url3"`
		At     string `json:"at"`
		Pretty string `json:"pretty"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if r.N != 3 || r.Name1 != "list" || r.URL1 != "https://api.example.com/items" || r.Auth != "Bearer abc" || r.Start1 != 6 {
		t.Errorf("request 1 = %+v", r)
	}
	if r.M2 != "POST" || r.Body2 != "{\n  \"name\": \"{{missing}}\"\n}" || r.CT != "application/json" {
		t.Errorf("request 2 = %+v", r)
	}
	if r.M3 != "GET" || r.URL3 != "https://example.com/plain" || r.At != "POST" {
		t.Errorf("request 3 / at_line = %+v", r)
	}
	if r.Pretty != "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": {},\n  \"c\": \"x,y\"\n}" {
		t.Errorf("pretty = %q", r.Pretty)
	}
}

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func stripANSIForTest(s string) string { return stripANSI(s) }

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

const mouseLeft = tea.MouseLeft
