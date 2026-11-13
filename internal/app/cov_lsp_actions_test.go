package app

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Coverage of symbols.go, lsp_actions.go, code_actions.go and
// code_actions_extra.go against a real nvim with an in-process fake language
// server (the same trick internal/nvim uses).

const covFakeLSPLua = `
local results = vim.json.decode(..., { luanil = { object = true, array = true } })
local function server(dispatchers)
  local closing, n = false, 0
  return {
    request = function(method, params, callback)
      n = n + 1
      local res
      if method == 'initialize' then
        res = { capabilities = {
          documentSymbolProvider = true, workspaceSymbolProvider = true,
          referencesProvider = true, hoverProvider = true,
          codeActionProvider = { resolveProvider = true },
          typeDefinitionProvider = true, implementationProvider = true,
          executeCommandProvider = { commands = { 'fake.cmd' } },
        } }
      elseif method == 'shutdown' then
        res = nil
      else
        res = results[method]
      end
      vim.schedule(function() callback(nil, res) end)
      return true, n
    end,
    notify = function(method)
      if method == 'exit' then closing = true end
      return true
    end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
local id = vim.lsp.start({ name = 'fakels', cmd = server, root_dir = vim.fn.getcwd() }, { bufnr = 0 })
local ok = vim.wait(5000, function()
  local c = vim.lsp.get_client_by_id(id)
  return c ~= nil and c.initialized
end)
return ok and 'ready' or 'not ready'
`

func covStartLSP(t *testing.T, m *Model, results map[string]any) {
	t.Helper()
	b, _ := json.Marshal(results)
	got, err := m.nvim.EvalLuaStringArgs(covFakeLSPLua, string(b))
	if err != nil || got != "ready" {
		t.Fatalf("fake LSP: %q %v", got, err)
	}
}

func lspRng(line, char int) map[string]any {
	p := map[string]any{"line": line, "character": char}
	return map[string]any{"start": p, "end": p}
}

// goRepoModel is a real-nvim model inside a git repo holding main.go.
func goRepoModel(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git: %v %s", err, out)
		}
	}
	src := "package main\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n\nfunc helper() {}\n\nfunc TestThing(t *testing.T) {\n\tx := 1\n}\n"
	file := filepath.Join(dir, "main_test.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "lib.go"), []byte("package main\n\nfunc Exported() {}\n\nvar x = 1\n"), 0o644)
	_ = exec.Command("git", "-C", dir, "add", ".").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-qm", "init").Run()
	m := newTestModel(t)
	if m.nvim == nil {
		t.Skip("no nvim")
	}
	m.w, m.h = 120, 40
	m.gitIsRepo = true
	return m, dir
}

func openAt(t *testing.T, m *Model, path string, line int) {
	t.Helper()
	if err := m.editor.Open(path); err != nil {
		t.Fatal(err)
	}
	_ = m.nvim.ExecLuaArgs(`vim.api.nvim_win_set_cursor(0, {..., 0})`, line)
	m.cursorLine = line
}

func TestCovSymbolsThroughFakeServer(t *testing.T) {
	m, dir := goRepoModel(t)
	lib := filepath.Join(dir, "lib.go")
	openAt(t, &m, lib, 3)
	uri := "file://" + lib
	covStartLSP(t, &m, map[string]any{
		"textDocument/documentSymbol": []any{
			map[string]any{"name": "Exported", "kind": 12, "range": lspRng(2, 0), "selectionRange": lspRng(2, 5),
				"children": []any{map[string]any{"name": "inner", "kind": 13, "range": lspRng(2, 9)}}},
		},
		"workspace/symbol": []any{
			map[string]any{"name": "x", "containerName": "main", "kind": 13, "location": map[string]any{"uri": uri, "range": lspRng(4, 4)}},
			map[string]any{"name": "nolines", "kind": 5, "location": map[string]any{"uri": uri}},
		},
		"textDocument/references":     []any{map[string]any{"uri": uri, "range": lspRng(4, 4)}},
		"textDocument/hover":          map[string]any{"contents": map[string]any{"kind": "markdown", "value": "**x** is an int"}},
		"textDocument/typeDefinition": []any{map[string]any{"uri": uri, "range": lspRng(4, 4)}},
	})

	// document symbols
	msg := m.fetchSymbolsCmd()().(SymbolPickerMsg)
	if len(msg.Items) != 2 || !strings.Contains(msg.Items[1].Title, "·") {
		t.Fatalf("items %+v", msg.Items)
	}
	m.applySymbolPickerMsg(msg)
	if !m.pickerOpen || m.pickerKind != pickerKindSymbols {
		t.Fatal("picker not open")
	}
	m.jumpToSymbol(msg.Items[0].ID)
	m.jumpToSymbol("unknown")
	if m.applySymbolPickerMsg(SymbolPickerMsg{}) == nil {
		t.Error("empty symbols must toast")
	}
	// hover: markdown -> preview doc
	hm := m.hoverCmd()()
	if pm, ok := hm.(PreviewMsg); !ok || pm.Title != "Hover" {
		t.Errorf("hover %#v", hm)
	}
	// references
	rmsg := m.fetchReferencesCmd()().(ReferencesPickerMsg)
	if len(rmsg.Items) != 1 || !strings.Contains(rmsg.Items[0].Title, "lib.go") {
		t.Fatalf("refs %+v", rmsg.Items)
	}
	m.pickerOpen = false
	m.applyReferencesPickerMsg(rmsg)
	if !m.pickerOpen {
		t.Error("refs picker")
	}
	if m.applyReferencesPickerMsg(ReferencesPickerMsg{}) == nil {
		t.Error("empty refs must toast")
	}
	// workspace symbols
	wmsg := m.fetchWorkspaceSymbolsCmd()().(WorkspaceSymbolPickerMsg)
	if len(wmsg.Items) != 2 {
		t.Fatalf("ws items %+v", wmsg.Items)
	}
	m.pickerOpen = false
	m.applyWorkspaceSymbolPickerMsg(wmsg)
	if !m.pickerOpen {
		t.Error("ws picker")
	}
	if m.applyWorkspaceSymbolPickerMsg(WorkspaceSymbolPickerMsg{}) == nil {
		t.Error("empty ws must toast")
	}

	// goto
	m.focus = FocusExplorer
	m.gotoTypeDefCmd()
	if m.focus != FocusEditor {
		t.Error("goto type should focus the editor")
	}
	if m.gotoImplementationCmd() == nil { // fake has no answer: "No implementation found"
		t.Error("expected a toast")
	}
	if m.afterGoto(false, errors.New("x"), "type definition") == nil {
		t.Error("error toast")
	}
	m.jumpToWorkspaceSymbol(wmsg.Items[0].ID)
	m.jumpToWorkspaceSymbol("nope")
	m.workspaceSymIndex["blank"] = workspaceSymbolTarget{}
	m.jumpToWorkspaceSymbol("blank")
	if !looksMarkdown("a **b**") || looksMarkdown("plain") {
		t.Error("looksMarkdown")
	}
	for k := 0; k <= 24; k++ {
		_ = symbolKindGlyph(k)
		_ = symbolKindLabel(k)
	}
}

func TestCovLSPNoServerAndNoNvim(t *testing.T) {
	m, dir := goRepoModel(t)
	openAt(t, &m, filepath.Join(dir, "lib.go"), 1)
	// no server attached: symbols come back empty, hover is empty
	if msg := m.fetchSymbolsCmd()(); msg == nil {
		t.Error("no-LSP symbols return an empty picker msg")
	}
	if _, ok := m.hoverCmd()().(hoverEmptyMsg); !ok {
		t.Error("hover with no server is empty")
	}
	if m.fetchCodeActionsCmd() == nil {
		t.Fatal("cmd")
	}
	if msg, ok := m.fetchCodeActionsCmd()().(CodeActionsMsg); !ok || len(msg.Actions) != 0 {
		t.Errorf("msg %+v", msg)
	}
	var e Model
	if e.fetchSymbolsCmd() != nil || e.fetchWorkspaceSymbolsCmd() != nil || e.hoverCmd() != nil || e.fetchReferencesCmd() != nil ||
		e.gotoTypeDefCmd() != nil || e.gotoImplementationCmd() != nil || e.fetchCodeActionsCmd() != nil ||
		e.generateDocCommentAt(1) != nil || e.enclosingGoFuncName(3) != "" {
		t.Error("no editor: nothing runs")
	}
	if _, ok := e.exportedSymbolOnLine(1); ok {
		t.Error("no editor")
	}
	if e.applySelectedCodeAction("ca-1") != nil || e.applyPreferredCodeActionFromCache() != nil {
		t.Error("no editor")
	}
}

func TestCovCodeActionPicker(t *testing.T) {
	m, dir := goRepoModel(t)
	file := filepath.Join(dir, "main_test.go")
	openAt(t, &m, file, 9)
	acts := []nvim.CodeAction{
		{Index: 1, Title: "Fix it\nsecond", Kind: "quickfix", Client: "gopls", IsPreferred: true},
		{Index: 2, Title: "Organize", Kind: "source.organizeImports"},
		{Index: 3, Title: "Extract", Kind: "refactor.extract"},
		{Index: 4, Title: "Generate stubs"},
		{Index: 5, Title: "Run test please"},
		{Index: 6, Title: "other", Kind: "weird"},
	}
	m.applyCodeActionsMsg(CodeActionsMsg{Actions: acts})
	if !m.pickerOpen || m.pickerKind != pickerKindCodeAction || m.preferredCodeAction == nil {
		t.Fatalf("picker open=%v preferred=%v", m.pickerOpen, m.preferredCodeAction)
	}
	// synth items exist (Go test file + git)
	sawSynth := false
	for _, v := range m.codeActionsIndex {
		if v < 0 {
			sawSynth = true
		}
	}
	if !sawSynth {
		t.Error("no synthesized actions")
	}
	// apply the LSP one (no entry parked in nvim: still ok), then preferred
	if m.applySelectedCodeAction("ca-1") == nil || m.preferredCodeAction != nil {
		t.Error("apply must toast and clear the hint")
	}
	if m.applySelectedCodeAction("nothing") != nil {
		t.Error("unknown id")
	}
	m.preferredCodeAction = &acts[0]
	if m.applyPreferredCodeActionFromCache() == nil || m.preferredCodeAction != nil {
		t.Error("preferred apply")
	}
	if m.applyPreferredCodeActionFromCache() != nil {
		t.Error("nothing cached")
	}
	// kinds -> group, icon, title, hint
	for _, k := range []string{"", "quickfix", "refactor.x", "source.fixAll", synthKindTestRun, synthKindTestCommand, synthKindTestDebug,
		synthKindGitBlame, synthKindGitDiff, synthKindGitCopy, synthKindGenerateDoc, synthKindGenerateTbl, "ai.x", "other"} {
		a := nvim.CodeAction{Kind: k, Title: "t", Client: "c"}
		_ = codeActionGroup(a)
		_ = codeActionIcon(k)
		_ = formatCodeActionTitle(a)
		_ = formatCodeActionHint(a)
	}
	if codeActionGroup(nvim.CodeAction{Title: "Generate x"}) != "Generate" || codeActionGroup(nvim.CodeAction{Title: "my test"}) != "Test" {
		t.Error("title heuristics")
	}
	// empty -> toast (nothing synth: no file)
	var empty Model
	empty.synthCodeActions = nil
	if empty.applyCodeActionsMsg(CodeActionsMsg{}) == nil {
		t.Error("no actions must toast")
	}
}

func TestCovSynthActionsGoFile(t *testing.T) {
	m, dir := goRepoModel(t)
	restore := SetClipboardSink(func(string) {})
	defer restore()
	file := filepath.Join(dir, "main_test.go")
	openAt(t, &m, file, 9) // inside TestThing
	extra := m.extraCodeActionsForContext()
	titles := map[string]int{}
	for _, a := range extra {
		titles[a.Title] = a.Index
	}
	for _, want := range []string{"Run Test: TestThing", "Debug Test: TestThing", "Copy go test command", "Run Package Tests", "Git: Show Blame for Line", "Git: Show Diff for File", "Git: Copy Commit Hash for Line"} {
		if _, ok := titles[want]; !ok {
			t.Errorf("missing %q in %v", want, titles)
		}
	}
	for _, a := range extra {
		m.synthCodeActions[a.Index] = m.synthCodeActions[a.Index]
		if strings.HasPrefix(a.Title, "Debug") || strings.HasPrefix(a.Title, "Run") && a.Title != "Run Package Tests" {
			continue // would start a runner / debugger; covered elsewhere
		}
		_ = m.applySynthCodeAction(a.Index)
	}
	if m.applySynthCodeAction(12345) != nil {
		t.Error("unknown synth index")
	}
	// A plain function on an exported declaration: doc comment + table test.
	lib := filepath.Join(dir, "lib.go")
	openAt(t, &m, lib, 3)
	extra = m.extraCodeActionsForContext()
	var doc, tbl int
	for _, a := range extra {
		switch {
		case strings.HasPrefix(a.Title, "Generate: Doc"):
			doc = a.Index
		case strings.HasPrefix(a.Title, "Generate: Table"):
			tbl = a.Index
		}
	}
	if doc == 0 {
		t.Fatalf("no doc action in %v", extra)
	}
	if m.applySynthCodeAction(doc) == nil {
		t.Error("doc comment toast")
	}
	if text, _ := m.nvim.EvalLuaString(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`); !strings.Contains(text, "// Exported describes") {
		t.Errorf("doc not inserted: %q", text)
	}
	if tbl != 0 {
		m.applySynthCodeAction(tbl)
	}
	if _, err := os.Stat(filepath.Join(dir, "lib_test.go")); err != nil && tbl != 0 {
		t.Errorf("scaffold missing: %v", err)
	}
	// generateDocComment edge cases: unexported, no symbol, bad line
	for _, line := range []int{6, 1, 0, 99} {
		m.generateDocCommentAt(line)
	}
	// blame / diff / copy edge cases
	if m.synthGitBlame("nope.go", 1) == nil {
		t.Error("blame error toast")
	}
	if m.synthGitCopyHash("nope.go", 1) == nil {
		t.Error("copy error toast")
	}
	if msg := m.synthGitDiff("lib.go")(); msg == nil {
		t.Error("diff")
	}
	_ = os.WriteFile(lib, []byte("package main\n\nfunc Exported() {}\n\nvar x = 2\n"), 0o644)
	if pm, ok := m.synthGitDiff("lib.go")().(PreviewMsg); !ok || !strings.Contains(pm.Title, "diff") {
		t.Errorf("diff msg %+v", pm)
	}
	if _, ok := m.synthGitDiff("main_test.go")().(PreviewMsg); !ok {
		t.Error("clean diff preview")
	}
	if m.synthGitBlame("lib.go", 1) == nil {
		t.Error("blame ok toast")
	}
	// untracked file has no hash
	_ = os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main\n"), 0o644)
	m.synthGitCopyHash("new.go", 1)
}

func TestCovTableTestScaffold(t *testing.T) {
	m := Model{}
	dir := t.TempDir()
	if m.generateTableTestSkeleton("", "") == nil {
		t.Error("needs a function")
	}
	src := filepath.Join(dir, "calc.go")
	_ = os.WriteFile(src, []byte("package calc\n"), 0o644)
	m.generateTableTestSkeleton("add", src)
	b, _ := os.ReadFile(filepath.Join(dir, "calc_test.go"))
	if !strings.Contains(string(b), "package calc") || !strings.Contains(string(b), "func TestAdd(") {
		t.Errorf("scaffold %q", b)
	}
	// existing file without trailing newline is appended to
	_ = os.WriteFile(filepath.Join(dir, "calc_test.go"), []byte("package calc"), 0o644)
	m.generateTableTestSkeleton("Sub", src)
	b, _ = os.ReadFile(filepath.Join(dir, "calc_test.go"))
	if !strings.Contains(string(b), "func TestSub(") || !strings.HasPrefix(string(b), "package calc\n") {
		t.Errorf("append %q", b)
	}
	// unreadable target (a directory): error toast
	_ = os.MkdirAll(filepath.Join(dir, "x_test.go"), 0o755)
	if m.generateTableTestSkeleton("f", filepath.Join(dir, "x.go")) == nil {
		t.Error("read error toast")
	}
	// unwritable dir
	if os.Geteuid() != 0 {
		ro := t.TempDir()
		_ = os.Chmod(ro, 0o555)
		defer os.Chmod(ro, 0o755)
		if m.generateTableTestSkeleton("f", filepath.Join(ro, "y.go")) == nil {
			t.Error("write error toast")
		}
	}
	for src, want := range map[string]string{"package x // c\n": "x", "package y/*c*/\n": "y", "nothing": "main"} {
		p := filepath.Join(dir, "p.go")
		_ = os.WriteFile(p, []byte(src), 0o644)
		if got := readPackageName(p); got != want {
			t.Errorf("%q -> %q", src, got)
		}
	}
	if readPackageName(filepath.Join(dir, "missing.go")) != "main" {
		t.Error("missing file")
	}
	var nm Model
	if nm.goPackageRel("/elsewhere/x.go") == "" {
		t.Error("rel")
	}
	t.Chdir(dir)
	if r := nm.goPackageRel(filepath.Join(dir, "sub", "x.go")); r != "./sub" {
		t.Errorf("rel = %q", r)
	}
	if r := nm.goPackageRel(filepath.Join(dir, "x.go")); r != "." {
		t.Errorf("rel = %q", r)
	}
	if r := nm.goPackageRel("/elsewhere/x.go"); r != "./..." {
		t.Errorf("rel = %q", r)
	}
	for name, ok := range map[string]bool{"Public": true, "private": false} {
		if _, got := parseExportedSymbol("func " + name + "() {}"); got != ok {
			t.Errorf("%s", name)
		}
	}
	if n, ok := parseExportedSymbol("type T struct{}"); !ok || n != "T" {
		t.Error("type")
	}
	if _, ok := parseExportedSymbol("x := 1"); ok {
		t.Error("not a decl")
	}
	if scanEnclosingGoFunc("a\nfunc (r *T) Run() {\n}\nb") != "Run" || scanEnclosingGoFunc("x") != "" {
		t.Error("enclosing")
	}
	if isGoTestFunc("") || !isGoTestFunc("FuzzX") || isGoTestFunc("helper") {
		t.Error("isGoTestFunc")
	}
}

func TestCovRoutePickerSelectCodeActions(t *testing.T) {
	// the picker item ids reach applySelectedCodeAction through the picker kind
	var m Model
	m.codeActionsIndex = map[string]int{"ca-1": 1}
	_ = picker.Item{}
	_ = tea.Cmd(nil)
	if m.applySelectedCodeAction("ca-1") != nil { // no nvim
		t.Error("no editor")
	}
}
