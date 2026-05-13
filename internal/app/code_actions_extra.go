package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/git"
	"termocode/internal/nvim"
	"termocode/internal/toast"
)

// Synthesized code-action kinds used by Phase 2 to extend the LSP-driven
// picker with non-LSP contextual actions. The grouping in code_actions.go
// (codeActionGroup) and the apply path (applySelectedCodeAction) both branch
// on these strings; keep them in sync.
const (
	synthKindTestRun     = "test.run"     // "Test" group
	synthKindTestCommand = "test.command" // "Test" group (clipboard)
	synthKindGitBlame    = "git.blame"    // "Git" group
	synthKindGitDiff     = "git.diff"     // "Git" group
	synthKindGitCopy     = "git.copy"     // "Git" group
	synthKindGenerateDoc = "generate.doc" // "Generate" group
	synthKindGenerateTbl = "generate.test"
)

// synthCodeAction carries the per-action payload our Phase-2 handlers need
// when the user picks a synthesized item. The Kind column matches the
// constants above and decides which Model handler runs in
// applySelectedCodeAction. Every other field is a free-form payload — most
// actions only need one or two of them.
type synthCodeAction struct {
	Kind     string
	FuncName string // e.g. "TestParse" or "Parse"
	PkgPath  string // package directory relative to cwd, e.g. "./internal/app" or "."
	Line     int    // 1-based line number (for git blame, doc-comment insertion)
	Path     string // absolute path of the file the action targets
	GitRel   string // repo-relative path (set for git.* kinds)
	Command  string // pre-rendered shell command (test.command kind copies this)
}

// extraCodeActionsForContext synthesises the Phase-2 contextual code actions:
// Run/Debug Test, Git Blame/Diff/Copy-Hash, and Generate Doc Comment. They
// are appended to whatever the LSP returned and then bucketed into the same
// picker groups as the rest. Returning nil/empty is fine — the picker just
// shows the LSP set.
//
// The returned indices are NEGATIVE so they don't collide with LSP's positive
// 1-based indices in nvim's parked _termocode_code_actions table. The apply
// path looks up the negative index in m.synthCodeActions to decide which
// Go-side handler to run.
func (m *Model) extraCodeActionsForContext() []nvim.CodeAction {
	if m.synthCodeActions == nil {
		m.synthCodeActions = map[int]synthCodeAction{}
	} else {
		// Wipe stale entries from the previous open so old indices can't
		// double-fire if the user rapidly reopens the picker on a new file.
		for k := range m.synthCodeActions {
			delete(m.synthCodeActions, k)
		}
	}

	path := m.editor.Path()
	line := m.cursorLine
	if line <= 0 {
		line = 1
	}

	var out []nvim.CodeAction
	idx := -1 // negative index sentinel; decremented for each new synth action

	addSynth := func(title, kind string, payload synthCodeAction) {
		payload.Kind = kind
		m.synthCodeActions[idx] = payload
		out = append(out, nvim.CodeAction{
			Index:       idx,
			Title:       title,
			Kind:        kind,
			Client:      "termocode",
			IsPreferred: false,
		})
		idx--
	}

	// ── 1. Test actions (Go files only) ───────────────────────────────────
	if path != "" && strings.HasSuffix(path, ".go") {
		funcName := m.enclosingGoFuncName(line)
		pkgRel := m.goPackageRel(path)

		// Per-test runner: only when the cursor is inside a Test/Bench/Example.
		if isGoTestFunc(funcName) {
			addSynth(
				"Run Test: "+funcName,
				synthKindTestRun,
				synthCodeAction{FuncName: funcName, PkgPath: pkgRel, Path: path},
			)
			addSynth(
				"Copy go test command",
				synthKindTestCommand,
				synthCodeAction{
					FuncName: funcName,
					PkgPath:  pkgRel,
					Command:  fmt.Sprintf("go test -run ^%s$ -v %s", funcName, pkgRel),
				},
			)
		}
		// Package-wide test runner is always available in Go files.
		addSynth(
			"Run Package Tests",
			synthKindTestRun,
			synthCodeAction{FuncName: "", PkgPath: pkgRel, Path: path},
		)
	}

	// ── 2. Git actions (only inside a repo with an open file) ─────────────
	if m.gitIsRepo && path != "" {
		if rel, ok := m.activeGitRel(); ok {
			addSynth(
				"Git: Show Blame for Line",
				synthKindGitBlame,
				synthCodeAction{Path: path, GitRel: rel, Line: line},
			)
			addSynth(
				"Git: Show Diff for File",
				synthKindGitDiff,
				synthCodeAction{Path: path, GitRel: rel},
			)
			addSynth(
				"Git: Copy Commit Hash for Line",
				synthKindGitCopy,
				synthCodeAction{Path: path, GitRel: rel, Line: line},
			)
		}
	}

	// ── 3. Generate actions (Go files only, lightweight templates) ────────
	if path != "" && strings.HasSuffix(path, ".go") {
		// Doc comment: only when the cursor sits ON a line that starts an
		// exported func/type/var/const declaration. The handler reads the
		// line live at apply-time so we don't need to ship the symbol name
		// in the payload.
		if name, ok := m.exportedSymbolOnLine(line); ok {
			addSynth(
				"Generate: Doc Comment for "+name,
				synthKindGenerateDoc,
				synthCodeAction{FuncName: name, Path: path, Line: line},
			)
		}
		// Table-driven test: only when the cursor is inside a function we
		// can target, and the file ISN'T already a _test.go.
		if !strings.HasSuffix(path, "_test.go") {
			if fn := m.enclosingGoFuncName(line); fn != "" && !isGoTestFunc(fn) {
				addSynth(
					"Generate: Table-Driven Test Skeleton for "+fn,
					synthKindGenerateTbl,
					synthCodeAction{FuncName: fn, Path: path, Line: line},
				)
			}
		}
	}

	return out
}

// applySynthCodeAction dispatches a synthesized (Phase-2) action by Kind.
// Called from applySelectedCodeAction when the picker ID maps to a negative
// index. Returns the tea.Cmd the caller forwards to the runtime.
func (m *Model) applySynthCodeAction(idx int) tea.Cmd {
	a, ok := m.synthCodeActions[idx]
	if !ok {
		return nil
	}
	switch a.Kind {
	case synthKindTestRun:
		return m.runGoTestExec(a.FuncName, a.PkgPath)
	case synthKindTestCommand:
		safeClipboardWrite(a.Command)
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Copied", a.Command)
		return toastCmd
	case synthKindGitBlame:
		return m.synthGitBlame(a.GitRel, a.Line)
	case synthKindGitDiff:
		return m.synthGitDiff(a.GitRel)
	case synthKindGitCopy:
		return m.synthGitCopyHash(a.GitRel, a.Line)
	case synthKindGenerateDoc:
		return m.generateDocCommentAt(a.Line)
	case synthKindGenerateTbl:
		return m.generateTableTestSkeleton(a.FuncName, a.Path)
	}
	return nil
}

// runGoTestExec suspends termocode and runs `go test` so the user sees real
// streaming output, then resumes on Enter / process exit. funcName == ""
// means the package-wide form (no -run filter, no -v).
func (m *Model) runGoTestExec(funcName, pkgPath string) tea.Cmd {
	if pkgPath == "" {
		pkgPath = "./..."
	}
	args := []string{"test"}
	if funcName != "" {
		args = append(args, "-run", "^"+funcName+"$", "-v", pkgPath)
	} else {
		args = append(args, pkgPath)
	}
	c := exec.Command("go", args...)
	c.Env = os.Environ()
	if cwd, err := os.Getwd(); err == nil {
		c.Dir = cwd
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			// `go test` exits non-zero on test failure. We surface it as a
			// soft warn-toast rather than ErrMsg so the user doesn't see a
			// scary red banner for a normal failed test run.
			return ToastMsg{Level: toast.Warn, Title: "go test exited non-zero", Body: err.Error()}
		}
		return ToastMsg{Level: toast.Info, Title: "go test finished"}
	})
}

// synthGitBlame is the Phase-2 inline-blame variant of gitBlameCurrentLine —
// same plumbing, but it always uses the synth action's payload (so the line
// number and rel path are pinned to the moment the picker opened).
func (m *Model) synthGitBlame(rel string, line int) tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	bl, err := git.BlameLineAt(cwd, rel, line)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Blame failed", err.Error())
		return toastCmd
	}
	body := fmt.Sprintf("%s · %s · %s · %s", bl.Hash, bl.Author, bl.When, bl.Subject)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, body)
	return toastCmd
}

// synthGitDiff opens the existing preview overlay with a colourised diff
// of the active file. Mirrors gitDiffActiveFile's plumbing.
func (m *Model) synthGitDiff(rel string) tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		raw, err := git.Diff(cwd, rel)
		if err != nil {
			return ErrMsg{Err: err}
		}
		body := colorizeDiff(raw)
		if strings.TrimSpace(raw) == "" {
			body = "(no differences)"
		}
		return PreviewMsg{Title: "diff · " + rel, Body: body}
	}
}

// synthGitCopyHash blames the line, then writes just the SHA to the
// clipboard. Useful for "I want to share this commit in chat".
func (m *Model) synthGitCopyHash(rel string, line int) tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	bl, err := git.BlameLineAt(cwd, rel, line)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Blame failed", err.Error())
		return toastCmd
	}
	if bl.Hash == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No commit hash for this line (uncommitted?)")
		return toastCmd
	}
	safeClipboardWrite(bl.Hash)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Copied", bl.Hash)
	return toastCmd
}

// generateDocCommentAt inserts a `// Symbol describes ...` doc comment on
// the line ABOVE the symbol declaration. Reads the current line live from
// nvim so the user doesn't get a stale insertion when they've moved or
// edited since opening the picker.
func (m *Model) generateDocCommentAt(line int) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	if line <= 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No symbol on this line")
		return toastCmd
	}
	// Read the declaration line live, derive the symbol name, then insert
	// the comment one line above. Single ExecLua so it's atomic from the
	// user's perspective (one undo step).
	luaSrc := fmt.Sprintf(`
		local row = %d
		local lines = vim.api.nvim_buf_get_lines(0, row-1, row, false)
		if #lines == 0 then return 'noline' end
		local name = lines[1]:match('^%%s*func%%s+%%(?[^)]*%%)?%%s*([%%w_]+)%%s*%%(')
		if not name then
			name = lines[1]:match('^%%s*type%%s+([%%w_]+)%%s')
		end
		if not name then
			name = lines[1]:match('^%%s*var%%s+([%%w_]+)%%s')
		end
		if not name then
			name = lines[1]:match('^%%s*const%%s+([%%w_]+)%%s')
		end
		if not name then return 'noname' end
		local first = name:sub(1,1)
		if not (first >= 'A' and first <= 'Z') then return 'unexported' end
		local stub = '// ' .. name .. ' describes ...'
		vim.api.nvim_buf_set_lines(0, row-1, row-1, false, { stub })
		return 'ok'
	`, line)
	out, err := m.nvim.EvalLuaString(luaSrc)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Insert failed", err.Error())
		return toastCmd
	}
	switch out {
	case "ok":
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "Inserted doc comment")
		return toastCmd
	case "unexported":
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "Symbol is not exported — skipped")
		return toastCmd
	default:
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No symbol on this line")
		return toastCmd
	}
}

// generateTableTestSkeleton appends a t.Run-style table-driven test for
// the function the cursor is on into the corresponding *_test.go file.
// We append rather than insert at cursor so the user's source file isn't
// polluted with test scaffolding.
func (m *Model) generateTableTestSkeleton(fn, srcPath string) tea.Cmd {
	if fn == "" || srcPath == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No function on this line")
		return toastCmd
	}
	// Compute target *_test.go path. If it already has _test.go we'd have
	// skipped at synthesis time; here we just swap the extension.
	dir := filepath.Dir(srcPath)
	base := filepath.Base(srcPath)
	if strings.HasSuffix(base, ".go") {
		base = strings.TrimSuffix(base, ".go") + "_test.go"
	}
	target := filepath.Join(dir, base)
	skeleton := tableTestSkeleton(fn)

	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Generate failed", err.Error())
		return toastCmd
	}
	var body string
	if len(existing) == 0 {
		// Brand-new test file: prepend a package decl. We sniff the
		// package name from the source file via a quick read.
		body = "package " + readPackageName(srcPath) + "\n\nimport \"testing\"\n\n" + skeleton
	} else {
		// Existing file: append the new test func at the bottom. Newline
		// guard so we don't smash onto the previous line.
		prefix := string(existing)
		if !strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		}
		body = prefix + "\n" + skeleton
	}
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Write failed", err.Error())
		return toastCmd
	}
	// Open the new test file so the user can iterate on the scaffold.
	if m.nvim != nil {
		_ = m.nvim.Command("edit " + target)
	}
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Test scaffold added", filepath.Base(target))
	return toastCmd
}

// tableTestSkeleton is the canonical t.Run table-driven test template.
// Kept dumb on purpose — Phase 3 is where AST-based parameter inference
// lives; for now the user fills in the cases.
func tableTestSkeleton(fn string) string {
	// Capitalise first letter of `fn` so we get TestParse from parse,
	// TestParse from Parse, TestX from x. ASCII-only — Go identifiers
	// rarely contain non-ASCII letters in our codebases.
	name := fn
	if name != "" && name[0] >= 'a' && name[0] <= 'z' {
		name = string(name[0]-'a'+'A') + name[1:]
	}
	testName := "Test" + name
	return fmt.Sprintf(`func %s(t *testing.T) {
	cases := []struct {
		name string
		// TODO: input / want fields
	}{
		{name: "happy path"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// TODO: call %s and assert
			_ = tc
		})
	}
}
`, testName, fn)
}

// readPackageName best-efforts the `package` clause out of a Go source
// file. Returns "main" on any failure so the generated test file at least
// compiles in isolation.
func readPackageName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "main"
	}
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "package ") {
			rest := strings.TrimPrefix(ln, "package ")
			rest = strings.TrimSpace(rest)
			if i := strings.IndexAny(rest, " \t/"); i > 0 {
				rest = rest[:i]
			}
			if rest != "" {
				return rest
			}
		}
	}
	return "main"
}

// goPackageRel returns the package directory of `path` expressed relative
// to the current working directory and prefixed with `./` so it's a valid
// `go test` target. Falls back to `./...` if the path is outside cwd.
func (m Model) goPackageRel(path string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return "./..."
	}
	dir := filepath.Dir(path)
	rel, err := filepath.Rel(cwd, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "./..."
	}
	if rel == "." {
		return "."
	}
	return "./" + rel
}

// goTestFuncRe matches `^func TestXxx(`, `^func BenchmarkXxx(`, and
// `^func ExampleXxx(`. Used by enclosingGoFuncName + isGoTestFunc to drive
// the "Run Test" affordance.
var goTestFuncPrefixes = []string{"Test", "Benchmark", "Example", "Fuzz"}

// isGoTestFunc reports whether `name` looks like a Go testing-package
// entry-point function (Test*, Benchmark*, Example*, Fuzz*). Empty name
// returns false; callers can use that as a "no enclosing func" sentinel.
func isGoTestFunc(name string) bool {
	if name == "" {
		return false
	}
	for _, p := range goTestFuncPrefixes {
		if strings.HasPrefix(name, p) {
			// Test, TestX, Benchmark, BenchmarkX, … all qualify. The Go
			// testing tool itself is happy with any suffix (including
			// none — `Test` alone is a valid test func), so we don't add
			// further constraints here.
			return true
		}
	}
	return false
}

// goFuncDeclRe matches a Go function declaration line, capturing the
// function name. Tolerates a receiver clause (`func (r *T) Name(...)`) and
// any whitespace. We only use it on lines we already think are at column 0
// so it's deliberately not anchored at end-of-line.
var goFuncDeclRe = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// enclosingGoFuncName scans backwards from the cursor line looking for the
// nearest `func Name(...)` declaration, returning the name. Returns ""
// when the scan hits BoF without finding a function — either the cursor
// is at file-scope or the file isn't Go-shaped.
//
// We pull buffer lines via nvim_buf_get_lines on a small backwards window
// (up to 800 lines, enough for most files; pathological 10k-line files
// just don't get the per-test affordance, which is fine).
func (m Model) enclosingGoFuncName(line int) string {
	if m.nvim == nil || line <= 0 {
		return ""
	}
	const window = 800
	start := line - window
	if start < 1 {
		start = 1
	}
	luaSrc := fmt.Sprintf(`
		local s, e = %d, %d
		local lines = vim.api.nvim_buf_get_lines(0, s-1, e, false)
		return table.concat(lines, '\n')
	`, start, line)
	body, err := m.nvim.EvalLuaString(luaSrc)
	if err != nil || body == "" {
		return ""
	}
	return scanEnclosingGoFunc(body)
}

// scanEnclosingGoFunc walks `body` from the bottom up and returns the
// first function-declaration name it hits. Pure-string operation so the
// helper can be exercised in unit tests without nvim.
func scanEnclosingGoFunc(body string) string {
	lines := strings.Split(body, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if m := goFuncDeclRe.FindStringSubmatch(lines[i]); m != nil {
			return m[1]
		}
	}
	return ""
}

// exportedSymbolOnLine inspects the buffer line and returns the symbol name
// when it starts an exported func/type/var/const. Used to gate the
// "Generate: Doc Comment for X" affordance.
func (m Model) exportedSymbolOnLine(line int) (string, bool) {
	if m.nvim == nil || line <= 0 {
		return "", false
	}
	luaSrc := fmt.Sprintf(`
		local lines = vim.api.nvim_buf_get_lines(0, %d-1, %d, false)
		if #lines == 0 then return '' end
		return lines[1]
	`, line, line)
	body, err := m.nvim.EvalLuaString(luaSrc)
	if err != nil || body == "" {
		return "", false
	}
	return parseExportedSymbol(body)
}

// parseExportedSymbol extracts a symbol name from a single Go declaration
// line and reports whether the symbol is exported (uppercase first letter).
// Pure helper for unit-test friendliness.
func parseExportedSymbol(line string) (string, bool) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
		regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s`),
		regexp.MustCompile(`^\s*var\s+([A-Za-z_][A-Za-z0-9_]*)\s`),
		regexp.MustCompile(`^\s*const\s+([A-Za-z_][A-Za-z0-9_]*)\s`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(line); m != nil {
			name := m[1]
			if name == "" {
				return "", false
			}
			first := name[0]
			if first >= 'A' && first <= 'Z' {
				return name, true
			}
			return "", false
		}
	}
	return "", false
}

// ToastMsg lets background commands (like tea.ExecProcess callbacks)
// surface a toast through the standard Update path. update.go's main
// switch handles it generically.
type ToastMsg struct {
	Level toast.Severity
	Title string
	Body  string
}
