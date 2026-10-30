//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The v2 pages under More (WEB-43 … WEB-46) against the fake hub.

// openMoreSub logs in and opens More › the link of sub, which asks for
// topic.
func openMoreSub(t *testing.T, vp Viewport, sub, topic string) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-more2")
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="` + sub + `"]')`)
	c.WaitSub(t, topic)
	return p, c
}

// Settings list the laptop's values by group, read only: a changed value
// shows its default, there is no field and no button that changes one.
func TestSettingsReadOnly(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openMoreSub(t, vp, "settings", "settings")
			c.Send(t, Snap("settings", 1, webproto.SettingsSnap{Items: []webproto.SettingItem{
				{Key: "theme", Group: "Appearance", Type: "string", Value: "one-dark", Default: "vscode-dark-plus", Description: "Color theme ID."},
				{Key: "tab_size", Group: "Editor", Type: "int", Value: "4", Default: "4"},
				{Key: "word_wrap", Group: "Editor", Type: "bool", Value: "<b>true</b>", Default: "false"},
			}}))
			p.WaitJS(`document.querySelectorAll(".kv-row").length === 3`)
			if n := Eval[int](p, `document.querySelectorAll(".settings .group-title").length`); n != 2 {
				t.Errorf("%d groups", n)
			}
			if v := p.Text(`.kv-row[data-key="theme"] .kv-value`); v != "one-dark" {
				t.Errorf("theme value %q", v)
			}
			if d := p.Text(`.kv-row[data-key="theme"] .kv-def`); d != "default vscode-dark-plus" {
				t.Errorf("theme default %q", d)
			}
			if n := Eval[int](p, `document.querySelectorAll('.kv-row[data-key="tab_size"] .kv-def').length`); n != 0 {
				t.Error("an unchanged value shows its default")
			}
			if n := Eval[int](p, `document.querySelectorAll(".settings b, .settings input, .settings select, .settings textarea, .settings .btn").length`); n != 0 {
				t.Fatalf("%d fields, buttons or HTML on the settings page", n)
			}
			if !strings.Contains(p.Text(".settings-note"), "Read only") {
				t.Errorf("note %q", p.Text(".settings-note"))
			}
			c.Send(t, Delta("settings", 2, webproto.SettingsSnap{Items: []webproto.SettingItem{{Key: "tab_size", Group: "Editor", Type: "int", Value: "8", Default: "4"}}}))
			p.WaitJS(`document.querySelectorAll(".kv-row").length === 1`)
			p.noOverflow("settings")
		})
	}
}

// MCP shows the server's state, the agents and their last calls; there
// is no button at all; not listening says why.
func TestMCPStatusReadOnly(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openMoreSub(t, vp, "mcp", "mcp")
			c.Send(t, Snap("mcp", 1, webproto.MCPSnap{Mode: "auto", Listening: true,
				Agents: []webproto.MCPAgent{{Name: "agent-x", Since: 1_700_000_000_000, Calls: 14, Asked: 2}, {Since: 1_700_000_000_000}},
				Calls:  []string{"12:03 agent-x · read_file · ran without asking"}}))
			p.WaitJS(`document.querySelectorAll(".mcp-agent").length === 2`)
			if s := p.Text(".mcp-state"); s != "Listening for outside agents." {
				t.Errorf("state %q", s)
			}
			if m := p.Text(".mcp-agent .mcp-meta"); !strings.Contains(m, "14 calls, 2 asked") {
				t.Errorf("meta %q", m)
			}
			if n := Eval[string](p, `document.querySelectorAll(".mcp-agent .mcp-name")[1].textContent`); n != "(no name yet)" {
				t.Errorf("unnamed agent %q", n)
			}
			if n := Eval[int](p, `document.querySelectorAll(".mcpv button, .mcpv input").length`); n != 0 {
				t.Fatalf("%d controls on the MCP page", n)
			}
			c.Send(t, Delta("mcp", 2, webproto.MCPSnap{Mode: "off", Why: "The MCP server is turned off.", Agents: []webproto.MCPAgent{}, Calls: []string{}}))
			p.WaitJS(`document.querySelector(".mcp-why")?.textContent === "The MCP server is turned off."`)
			if s := p.Text(".mcp-state"); s != "Not listening." {
				t.Errorf("state %q", s)
			}
			p.noOverflow("mcp")
		})
	}
}
