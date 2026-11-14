package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Coverage of lsp_manager.go: the confirm cards, the background install with
// a fake installer, its events and the error toasts.

func toolsHome(t *testing.T) {
	t.Helper()
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
}

func TestCovToolRowSelectedStates(t *testing.T) {
	toolsHome(t)
	m := Model{w: 120, h: 40, toast: toast.New()}
	if m.onToolRowSelected("nope") != nil {
		t.Error("unknown tool")
	}
	tool, _ := lspinstall.Lookup("gopls")
	// installing: a toast
	m.ensureLSPMgr()
	m.lspMgr.installing["gopls"] = true
	if m.onToolRowSelected("gopls") == nil {
		t.Error("installing toasts")
	}
	delete(m.lspMgr.installing, "gopls")
	// missing: an install card (needs the go toolchain; the test host has it)
	fakeBin(t, nil)
	cmd := m.onToolRowSelected("gopls")
	if m.confirmOpen {
		t.Fatalf("no go on PATH must toast, not open a card")
	}
	if cmd == nil {
		t.Error("missing toolchain toasts")
	}
	fakeBin(t, map[string]string{"go": "exit 0", "gopls": "exit 0"})
	m.onToolRowSelected("gopls")
	if !m.confirmOpen || m.confirmKind != confirmKindToolInstall || !strings.Contains(m.confirm.View(), "found on PATH") {
		t.Errorf("system card: open=%v", m.confirmOpen)
	}
	// missing and installable
	fakeBin(t, map[string]string{"go": "exit 0"})
	m.confirmOpen = false
	m.onToolRowSelected("gopls")
	if !m.confirmOpen || m.lspMgr.pending != "gopls" {
		t.Error("install card")
	}
	// managed: reinstall / uninstall card
	bin, _ := lspinstall.BinDir()
	_ = os.MkdirAll(bin, 0o755)
	_ = os.WriteFile(filepath.Join(bin, tool.Bin), []byte("#!/bin/sh\n"), 0o755)
	m.confirmOpen = false
	m.onToolRowSelected("gopls")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Uninstall") {
		t.Error("managed card")
	}
	// uninstall through the confirm buttons
	msg := m.onToolConfirm("uninstall")().(lspUninstallDoneMsg)
	if msg.err != nil || msg.name != "gopls" {
		t.Errorf("uninstall %+v", msg)
	}
	m.onToolUninstalled(msg)
	m.onToolUninstalled(lspUninstallDoneMsg{name: "x", err: errors.New("no")})
	if m.onToolConfirm("install") != nil { // pending was cleared
		t.Error("no pending tool")
	}
	m.lspMgr.pending = "gopls"
	if m.onToolConfirm("cancel") != nil {
		t.Error("cancel")
	}
}

func TestCovToolInstallFlow(t *testing.T) {
	toolsHome(t)
	m := Model{w: 120, h: 40, toast: toast.New()}
	gopls, _ := lspinstall.Lookup("gopls")
	restore := SetToolInstaller(func(ctx context.Context, tl lspinstall.Tool, progress lspinstall.Progress) (string, error) {
		progress("downloading")
		progress("done")
		return "installed", nil
	})
	defer restore()
	m.ensureLSPMgr()
	m.lspMgr.pending = "gopls"
	cmd := m.onToolConfirm("install")
	if cmd == nil || !m.lspMgr.installing["gopls"] {
		t.Fatal("install not started")
	}
	if m.startToolInstall(gopls) != nil {
		t.Error("second start while installing")
	}
	// pump the events: a progress line, then the result
	ch := make(chan lspInstallEventMsg, 4)
	_ = ch
	ev := waitLSPInstall(nil)
	_ = ev
	var final lspInstallEventMsg
	var next tea.Cmd
	for _, c := range flattenBatch(cmd) {
		if msg, ok := c().(lspInstallEventMsg); ok {
			next = m.onToolInstallEvent(msg)
			final = msg
			break
		}
	}
	for i := 0; i < 5 && !final.done; i++ {
		for _, c := range flattenBatch(next) {
			if msg, ok := c().(lspInstallEventMsg); ok {
				next = m.onToolInstallEvent(msg)
				final = msg
				break
			}
		}
	}
	if m.lspMgr.installing["gopls"] {
		t.Fatal("install did not finish")
	}
	if m.lspMgr.results["gopls"].err != nil {
		t.Errorf("result %+v", m.lspMgr.results["gopls"])
	}
	// closed channel -> nil
	closed := make(chan lspInstallEventMsg)
	close(closed)
	if waitLSPInstall(closed)() != nil {
		t.Error("closed channel gives nil")
	}
}

// flattenBatch runs a tea.Batch result into its commands.
func flattenBatch(c tea.Cmd) []tea.Cmd {
	if c == nil {
		return nil
	}
	msg := c()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Cmd
		for _, x := range b {
			out = append(out, x)
		}
		return out
	}
	return []tea.Cmd{func() tea.Msg { return msg }}
}

func TestCovToolInstallResults(t *testing.T) {
	toolsHome(t)
	m := newTestModel(t)
	m.w, m.h = 120, 40
	m.ensureLSPMgr()
	// generic failure: preview in the batch; AI-started: just a toast
	m.lspMgr.installing["gopls"] = true
	if m.onToolInstallEvent(lspInstallEventMsg{name: "gopls", done: true, err: errors.New("boom"), output: "out"}) == nil {
		t.Error("failure toast")
	}
	m.lspMgr.byAI["gopls"] = true
	if m.onToolInstallEvent(lspInstallEventMsg{name: "gopls", done: true, err: errors.New("boom")}) == nil {
		t.Error("ai failure toast")
	}
	// success: LSP restarts servers, DAP registers adapters and clears the Run hint
	d := m.ensureDebug()
	d.setHint("DAP: dlv missing", "go")
	if m.onToolInstallEvent(lspInstallEventMsg{name: "gopls", done: true}) == nil {
		t.Error("lsp success toast")
	}
	d.hintTool = "dlv"
	d.hint = "x"
	if m.onToolInstallEvent(lspInstallEventMsg{name: "dlv", done: true}) == nil || d.hint != "" {
		t.Errorf("dap success: hint %q", d.hint)
	}
	// an agent CLI installed by hand opens its card; installed by the AI it only toasts
	m.confirmOpen = false
	m.onToolInstallEvent(lspInstallEventMsg{name: "claude-code", done: true})
	m.lspMgr.byAI["claude-code"] = true
	m.onToolInstallEvent(lspInstallEventMsg{name: "claude-code", done: true})
	// progress
	if m.onToolInstallEvent(lspInstallEventMsg{name: "gopls", line: "step"}) == nil || m.lspMgr.progress["gopls"] != "step" {
		t.Error("progress")
	}
	// error toast kinds
	tl, _ := lspinstall.Lookup("gopls")
	for _, e := range []error{
		&lspinstall.MissingToolchainError{Tool: "gopls", Bin: "go", Hint: "install go"},
		&lspinstall.OfflineError{Tool: "gopls"},
		&lspinstall.UnsupportedError{Tool: "gopls", Platform: "plan9", Hint: "build it"},
		errors.New("generic"),
	} {
		if m.toolErrorToast(tl, e, "") == nil {
			t.Errorf("%T", e)
		}
	}
}

func TestCovToolManagerPickerAndSuggest(t *testing.T) {
	toolsHome(t)
	fakeBin(t, nil)
	m := Model{w: 120, h: 40, toast: toast.New()}
	if m.openToolManager(lspinstall.CategoryLSP) == nil && !m.pickerOpen {
		t.Error("manager opens")
	}
	m.pickerOpen = false
	m.openToolManager(lspinstall.CategoryDAP)
	for _, st := range []lspinstall.State{lspinstall.Managed, lspinstall.System, lspinstall.Missing} {
		g, h := toolStateLabel(lspinstall.Tool{}, lspinstall.Status{State: st}, false)
		if g == "" || h == "" {
			t.Error("label")
		}
	}
	if _, h := toolStateLabel(lspinstall.Tool{}, lspinstall.Status{State: lspinstall.Managed}, false); h != "managed" {
		t.Errorf("hint %q", h)
	}
	// suggest: a missing server for lua once; nothing without state
	if (&Model{}).maybeSuggestLanguageServer("lua") != nil {
		t.Error("no state")
	}
	m.ensureLSPMgr()
	m.lspMgr.installing["lua_ls"] = true
	if m.maybeSuggestLanguageServer("lua") != nil {
		t.Error("installing")
	}
	// clients chip
	m.setLSPClients([]string{"gopls"})
	if len(m.lspClients()) != 1 {
		t.Error("clients")
	}
	var nm Model
	nm.setLSPClients([]string{"x"})
	if nm.lspClients() != nil {
		t.Error("no manager")
	}
	_ = m.hitStatusLSPChip(0, 0)
	x, w := m.statusBarRect()
	_, _ = x, w
}
