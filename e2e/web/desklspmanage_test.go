//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// LSP: Manage Language Servers… (WEBP-2): the laptop's language servers
// in a picker with their state (the workspace's own first); a missing
// one is installed and termocode's own copy is reinstalled or removed,
// each asked here first (Cancel first), then by a laptop card.
func TestDeskManageLanguageServers(t *testing.T) {
	p, c := openFiles(t, Desktop)
	list := webproto.LSPServerList{Items: []webproto.LSPServer{
		{Name: "gopls", Title: "gopls (Go)", Langs: []string{"go"}, State: "managed", Version: "v0.16.2", Used: true},
		{Name: "pyright", Title: "pyright (Python)", Langs: []string{"python"}, State: "missing"},
		{Name: "clangd", Title: "clangd (C/C++)", Langs: []string{"c"}, State: "system"},
		{Name: "taplo", Title: "taplo (TOML)", Langs: []string{"toml"}, State: "missing", Problem: "cargo is not on PATH (install Rust)"},
	}}
	runCmd(t, p, c, true, "manage language servers", "LSP: Manage Language Servers…")
	reply(t, c, waitReq(t, c, webproto.OpLSPServers), list)
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join() === "gopls (Go),pyright (Python),clangd (C/C++),taplo (TOML)"`)
	details := `[...document.querySelectorAll(".quick-row")].map((e) => e.querySelector(".quick-detail")?.textContent ?? "").join("|")`
	p.WaitJS(details + ` === "Installed by termocode v0.16.2 · used in this workspace|Not installed|On the laptop's PATH|Not installed · cargo is not on PATH (install Rust)"`)
	p.Key("pyright")
	pickRow(p, "pyright (Python)")
	if got := p.Text(".ask .ask-text"); got != "Install pyright on the laptop? The laptop card shows what runs there." {
		t.Errorf("question %q", got)
	}
	askTheLaptop(t, p, c, webproto.OpLSPServerAsk, `{"name":"pyright","action":"install"}`)
	p.TapJS(`document.querySelector(".waiting .btn")`)
	waitReq(t, c, webproto.OpCardsAnswer)

	runCmd(t, p, c, false, "manage language servers", "LSP: Manage Language Servers…")
	reply(t, c, waitReq(t, c, webproto.OpLSPServers), list)
	p.WaitJS(`document.querySelector(".quick-row .quick-label")`)
	p.Key("gopls")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "gopls (Go)"`)
	p.Key("\r")
	// The actions of termocode's own copy open in the next list.
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join() === "Reinstall,Uninstall"`)
	p.Key("uninstall")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Uninstall"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".ask .ask-text")?.textContent === "Uninstall gopls from the laptop? Only termocode's own copy is removed."`)
	askTheLaptop(t, p, c, webproto.OpLSPServerAsk, `{"name":"gopls","action":"uninstall"}`)
}
