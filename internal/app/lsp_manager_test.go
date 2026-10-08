package app

import (
	"errors"
	"strings"
	"testing"

	"termocode/internal/lspinstall"
)

func TestToolPickerItemsStates(t *testing.T) {
	status := func(tl lspinstall.Tool) lspinstall.Status {
		switch tl.Name {
		case "gopls":
			return lspinstall.Status{State: lspinstall.Managed, Version: "latest"}
		case "clangd":
			return lspinstall.Status{State: lspinstall.System, Path: "/usr/bin/clangd"}
		}
		return lspinstall.Status{State: lspinstall.Missing}
	}
	items := toolPickerItems(lspinstall.CategoryLSP, map[string]bool{"pyright": true}, status)
	byID := map[string]string{}
	for _, it := range items {
		byID[it.ID] = it.Title + " | " + it.Hint
	}
	checks := map[string]string{
		"gopls":   "✓ gopls (Go) | managed · latest",
		"clangd":  "✓ clangd (C/C++) | on PATH",
		"pyright": "… pyright (Python) | installing…",
		"pylsp":   "✗ pylsp (Python) | missing · pip",
	}
	for id, want := range checks {
		if byID[id] != want {
			t.Errorf("%s: got %q want %q", id, byID[id], want)
		}
	}
	for _, it := range items {
		if it.ID == "dlv" {
			t.Error("debug adapters must not show in the LSP manager")
		}
	}
	dap := toolPickerItems(lspinstall.CategoryDAP, nil, status)
	if len(dap) != 3 {
		t.Fatalf("want 3 adapters, got %d", len(dap))
	}
}

func TestToolErrorToastKinds(t *testing.T) {
	var m Model
	tl, _ := lspinstall.Lookup("ts_ls")

	if cmd := m.toolErrorToast(tl, &lspinstall.MissingToolchainError{Tool: "ts_ls", Bin: "npm", Hint: "install Node.js"}, ""); cmd == nil {
		t.Fatal("want toast cmd")
	}
	if cmd := m.toolErrorToast(tl, &lspinstall.OfflineError{Tool: "ts_ls"}, ""); cmd == nil {
		t.Fatal("want toast cmd")
	}
	if cmd := m.toolErrorToast(tl, errors.New("exit status 1"), "npm ERR! boom"); cmd == nil {
		t.Fatal("want cmd")
	}
	p := installFailurePreview("ts_ls", errors.New("exit status 1"), "npm ERR! boom")
	if !strings.Contains(p.Title, "ts_ls") || !strings.Contains(p.Body, "exit status 1") || !strings.Contains(p.Body, "npm ERR! boom") {
		t.Fatalf("preview: %+v", p)
	}
}

func TestMaybeSuggestLanguageServerOnce(t *testing.T) {
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	t.Setenv("PATH", t.TempDir()) // nothing installed
	var m Model
	m.ensureLSPMgr()
	if m.maybeSuggestLanguageServer("lua") == nil {
		t.Fatal("first open of lua should suggest lua_ls")
	}
	m.lspMgr.lastLang = "" // simulate switching away and back
	if m.maybeSuggestLanguageServer("lua") != nil {
		t.Fatal("must only ask once")
	}
	if !lspinstall.LoadAsked()["lua"] {
		t.Fatal("asked state not persisted")
	}
}
