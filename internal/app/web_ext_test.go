package app

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/editor"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webExtRig is a web rig with a real nvim and the extension "good": a
// panel with three styled lines and a select handler, and a status item
// whose command sets a variable.
func webExtRig(t *testing.T) *webFilesRig {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	r := newWebFilesRig(t)
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	writeExt(t, filepath.Join(cfg, "termocode", "extensions"), "good", `
local tc = termocode
tc.register_command({ id = 'hello', title = 'Say Hello', run = function() vim.g.good_ran = 42 end })
tc.register_panel({ id = 'main', title = 'Good', icon = 'G', render = function(w, h)
  return { '{{accent bold}}width ' .. w .. '{{/}} rest', 'row two', 'GET {{host}}' }
end, on_select = function(row, line) vim.g.good_selected = row .. ':' .. line end })
tc.register_status_item({ id = 'st', text = function() return '{{muted}}st ok{{/}}' end, command = 'hello' })
`, nil)
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	r.m.nvim, r.m.ext, r.m.editor = c, newExtState(), editor.New(c)
	r.m.activity, r.m.showExp, r.m.explorerWidth = activity.New(), true, 30
	t.Cleanup(func() { activity.SetExtraItems(nil) })
	loadExtensions(t, r.m)
	poll(t, r.m) // the status items' text
	return r
}

// Spec (WEBP-7): the status topic names the extension panels (id, title,
// extension) and the status items with their text now (plain, no
// markup) and whether a click runs something.
func TestWebExtStatus(t *testing.T) {
	r := webExtRig(t)
	s := r.m.webStatus(&webStatusPub{})
	if s.Ext == nil || len(s.Ext.Panels) != 1 || s.Ext.Panels[0] != (webproto.StatusExtPanel{ID: "good.main", Title: "Good", Ext: "good"}) {
		t.Fatalf("panels %+v", s.Ext)
	}
	if len(s.Ext.Items) != 1 || s.Ext.Items[0] != (webproto.StatusExtItem{ID: "good.st", Text: "st ok", Run: true}) {
		t.Fatalf("items %+v", s.Ext.Items)
	}
	r.m.ext.reg.Panels, r.m.ext.reg.Status = nil, nil
	if s := r.m.webStatus(&webStatusPub{}); s.Ext != nil {
		t.Errorf("no extensions, yet %+v", s.Ext)
	}
}

// Spec (WEBP-7): ext.panel renders a panel's lines as styled spans (the
// markup's style names; a tag the markup does not know stays text);
// ext.select runs the panel's select handler with that row, as a click
// on the laptop does; ext.status_run runs a status item's command. An
// unknown panel or item is refused.
func TestWebExtPanelAndClicks(t *testing.T) {
	r := webExtRig(t)
	var v webproto.ExtPanelView
	webResData(t, r.call(webproto.OpExtPanel, &webproto.ExtIDArgs{ID: "good.main"}), &v)
	if len(v.Lines) != 3 {
		t.Fatalf("lines %+v", v.Lines)
	}
	if l := v.Lines[0]; len(l) != 2 || l[0] != (webproto.ExtSpan{T: "width 60", S: "accent bold"}) || l[1] != (webproto.ExtSpan{T: " rest"}) {
		t.Errorf("line 1 %+v", l)
	}
	if l := v.Lines[2]; len(l) != 1 || l[0].T != "GET {{host}}" {
		t.Errorf("line 3 %+v", l)
	}
	if f := r.call(webproto.OpExtSelect, &webproto.ExtSelectArgs{ID: "good.main", Row: 1}); f["t"] != "res" {
		t.Fatal(f)
	}
	if got := evalLua(t, r.m, "return tostring(vim.g.good_selected)"); got != "2:row two" {
		t.Errorf("select ran with %q", got)
	}
	if f := r.call(webproto.OpExtStatusRun, &webproto.ExtIDArgs{ID: "good.st"}); f["t"] != "res" {
		t.Fatal(f)
	}
	if got := evalLua(t, r.m, "return tostring(vim.g.good_ran)"); got != "42" {
		t.Errorf("the status item's command did not run: %q", got)
	}
	for op, args := range map[string]any{
		webproto.OpExtPanel:     &webproto.ExtIDArgs{ID: "good.nope"},
		webproto.OpExtSelect:    &webproto.ExtSelectArgs{ID: "good.nope"},
		webproto.OpExtStatusRun: &webproto.ExtIDArgs{ID: "good.nope"},
	} {
		if f := r.call(op, args); webErrCode(f) != "not_found" {
			t.Errorf("%s of an unknown id: %v", op, f)
		}
	}
	if f := r.call(webproto.OpExtSelect, &webproto.ExtSelectArgs{ID: "good.main", Row: 9}); webErrCode(f) != "not_found" {
		t.Errorf("a row the panel does not have: %v", f)
	}
}
