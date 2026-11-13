//go:build unix

package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webLSPRig is a workspace with a Go file, a tools folder where gopls is
// installed by termocode (v0.16.2), a PATH that holds clangd, and an
// installer that only records what it was asked to install.
func webLSPRig(t *testing.T) (*webFilesRig, string, chan string) {
	t.Helper()
	r := newWebFilesRig(t)
	tools := t.TempDir()
	t.Setenv(lspinstall.ToolsDirEnv, tools)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(r.root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gopls, _ := lspinstall.Lookup("gopls")
	clangd, _ := lspinstall.Lookup("clangd")
	for _, p := range []string{filepath.Join(tools, "bin", gopls.Bin), filepath.Join(bin, clangd.Bin)} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(tools, "gopls"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, "gopls", ".termocode-version"), []byte("v0.16.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	asked := make(chan string, 4)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	t.Cleanup(SetToolInstaller(func(ctx context.Context, tool lspinstall.Tool, _ lspinstall.Progress) (string, error) {
		asked <- tool.Name
		select {
		case <-block:
		case <-ctx.Done():
		}
		return "", nil
	}))
	return r, tools, asked
}

func webLSPFind(list webproto.LSPServerList, name string) (webproto.LSPServer, bool) {
	for _, s := range list.Items {
		if s.Name == name {
			return s, true
		}
	}
	return webproto.LSPServer{}, false
}

// Spec (WEBP-2): lsp.servers lists the laptop's language servers (never
// a debug adapter or agent CLI) with their state: installed by termocode
// (with its version), on the laptop's PATH, or missing; the ones for the
// workspace's languages come first. No laptop path goes to the web.
func TestWebLSPServersList(t *testing.T) {
	r, tools, _ := webLSPRig(t)
	var list webproto.LSPServerList
	webResData(t, r.call(webproto.OpLSPServers, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != len(lspinstall.ByCategory(lspinstall.CategoryLSP)) {
		t.Fatalf("%d rows, want every language server: %+v", len(list.Items), list.Items)
	}
	if list.Items[0].Name != "gopls" || !list.Items[0].Used {
		t.Errorf("the workspace's own language does not come first: %+v", list.Items[0])
	}
	if g := list.Items[0]; g.State != "managed" || g.Version != "v0.16.2" || g.Title != "gopls (Go)" || len(g.Langs) == 0 || g.Langs[0] != "go" {
		t.Errorf("gopls %+v", g)
	}
	if c, _ := webLSPFind(list, "clangd"); c.State != "system" || c.Used {
		t.Errorf("clangd %+v", c)
	}
	if p, _ := webLSPFind(list, "pyright"); p.State != "missing" {
		t.Errorf("pyright %+v", p)
	}
	if _, ok := webLSPFind(list, "dlv"); ok {
		t.Error("a debug adapter is in the language server list")
	}
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), tools) || strings.Contains(string(b), os.Getenv("PATH")) {
		t.Errorf("a laptop path went to the web: %s", b)
	}
}

// Spec (WEBP-2): installing a language server from the web waits for a
// laptop card that says what runs (the installer's command); after the
// laptop's yes it installs in the background, as the laptop's own
// manager does, and the list says it is installing.
func TestWebLSPServerInstallWaitsForTheLaptop(t *testing.T) {
	r, tools, asked := webLSPRig(t)
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpLSPServerAsk, &webproto.LSPServerAskArgs{Name: "pyright", Action: "install"}), &ref)
	if ref.Card == "" {
		t.Fatal("no laptop card")
	}
	select {
	case n := <-asked:
		t.Fatalf("%s installed before the laptop said yes", n)
	default:
	}
	v := r.m.confirm.View()
	if !strings.Contains(v, "Install pyright") || !strings.Contains(v, "npm") {
		t.Errorf("the card does not say what runs:\n%s", v)
	}
	if strings.Contains(v, tools) {
		t.Errorf("the card names the laptop's tools path (the web sees cards):\n%s", v)
	}
	webTestLaptopSelect(t, r.m, "confirm")
	select {
	case n := <-asked:
		if n != "pyright" {
			t.Fatalf("installed %s", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the install did not start after the laptop's yes")
	}
	var list webproto.LSPServerList
	webResData(t, r.call(webproto.OpLSPServers, &webproto.EmptyArgs{}), &list)
	if p, _ := webLSPFind(list, "pyright"); p.State != "installing" {
		t.Errorf("pyright %+v", p)
	}
	if f := r.call(webproto.OpLSPServerAsk, &webproto.LSPServerAskArgs{Name: "pyright", Action: "install"}); webErrCode(f) != "conflict" {
		t.Errorf("a second install while one runs: %v", f)
	}
}

// Spec (WEBP-2): uninstalling removes only a copy termocode installed,
// after a laptop card; a server on the laptop's PATH, one that is not
// installed and a name that is not a language server are refused.
func TestWebLSPServerUninstall(t *testing.T) {
	r, tools, _ := webLSPRig(t)
	for name, code := range map[string]string{"clangd": "conflict", "pyright": "conflict", "dlv": "not_found", "nope": "not_found"} {
		if f := r.call(webproto.OpLSPServerAsk, &webproto.LSPServerAskArgs{Name: name, Action: "uninstall"}); webErrCode(f) != code {
			t.Errorf("uninstall %s: want %s, got %v", name, code, f)
		}
	}
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpLSPServerAsk, &webproto.LSPServerAskArgs{Name: "gopls", Action: "uninstall"}), &ref)
	if v := r.m.confirm.View(); !strings.Contains(v, "Uninstall gopls") {
		t.Errorf("card:\n%s", v)
	}
	if _, err := os.Stat(filepath.Join(tools, "gopls")); err != nil {
		t.Fatal("removed before the laptop said yes")
	}
	webTestLaptopSelect(t, r.m, "confirm")
	if _, err := os.Stat(filepath.Join(tools, "gopls")); !os.IsNotExist(err) {
		t.Errorf("gopls is still there after the laptop's yes: %v", err)
	}
}
