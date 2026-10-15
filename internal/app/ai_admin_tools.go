package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/toast"
)

// K8: the agent manages the IDE's own setup.
//
//   - lsp_servers / dap_adapters (op list | install | uninstall): list is
//     auto (the servers / adapters for the workspace's languages and
//     their state). install asks with class run (it downloads and runs
//     code) and runs in the background like the manager picker; the tool
//     waits (bounded, Stop cancels the wait, not the install) or says
//     "check again with list". uninstall always asks (class "", no
//     "Always allow"): it deletes termocode's copy.
//   - extensions (op list | run | reload | log): list and log are auto;
//     run and reload run extension code, so they ask with class run.
//   - workspace_folders (op list | add | remove | open): list is auto;
//     add / remove ask with class ide. open is always refused: opening
//     another workspace re-execs termocode and would end this chat.

var (
	aiInstallOps = []string{"list", "install", "uninstall"}
	aiExtOps     = []string{"list", "run", "reload", "log"}
	aiFolderOps  = []string{"list", "add", "remove", "open"}
)

const (
	aiInstallWait     = 60 * time.Second // default wait for an install
	aiInstallPoll     = 200 * time.Millisecond
	aiInstallOutLines = 15
	aiExtReloadWait   = 15 * time.Second
	aiExtLogLines     = 100
	aiLangScanMax     = 20000 // files and folders looked at for the languages
)

// ── Installer seam (tests use a fake package source) ─────────────────────

type toolInstallerFns struct {
	// check fails when the tool can not be installed on this host
	// (missing toolchain, no recipe for the platform).
	check   func(lspinstall.Tool) error
	install func(context.Context, lspinstall.Tool, lspinstall.Progress) (string, error)
}

var toolInstallerSeam atomic.Pointer[toolInstallerFns]

// toolInstaller is the installer the manager and the AI tools use.
func toolInstaller() toolInstallerFns {
	if p := toolInstallerSeam.Load(); p != nil {
		return *p
	}
	return toolInstallerFns{check: toolInstallCheck, install: lspinstall.Install}
}

// toolInstallCheck builds the install plan and checks the host toolchain.
func toolInstallCheck(t lspinstall.Tool) error {
	dir, err := lspinstall.ToolDir(t.Name)
	if err != nil {
		return err
	}
	plan, err := lspinstall.BuildPlan(t, dir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	return lspinstall.CheckRequires(plan)
}

// SetToolInstaller replaces the LSP / DAP installer with install (no
// host checks) until restore is called. For tests: no network.
func SetToolInstaller(install func(context.Context, lspinstall.Tool, lspinstall.Progress) (string, error)) (restore func()) {
	old := toolInstallerSeam.Swap(&toolInstallerFns{check: func(lspinstall.Tool) error { return nil }, install: install})
	return func() { toolInstallerSeam.Store(old) }
}

// ── Workspace languages ──────────────────────────────────────────────────

// aiFiletypeByExt maps a file extension (or a whole file name) to the nvim
// filetype the installer registry uses.
var aiFiletypeByExt = map[string]string{
	".go": "go", "go.mod": "gomod", "go.work": "gowork", ".gotmpl": "gotmpl",
	".py": "python", ".pyi": "python",
	".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "javascriptreact",
	".ts": "typescript", ".mts": "typescript", ".cts": "typescript", ".tsx": "typescriptreact",
	".rs": "rust",
	".c":  "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hxx": "cpp", ".hh": "cpp",
	".m": "objc", ".mm": "objcpp",
	".lua": "lua",
	".sh":  "sh", ".bash": "sh",
	".toml": "toml",
}

// aiSkipDirs are folders nobody edits by hand.
var aiSkipDirs = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true,
	"build": true, "__pycache__": true, "venv": true}

// aiWorkspaceFiletypes are the filetypes of the files under root, sorted.
// Hidden folders and aiSkipDirs are left out; at most aiLangScanMax
// entries are looked at.
func aiWorkspaceFiletypes(root string) []string {
	seen := map[string]bool{}
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if n++; n > aiLangScanMax {
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || aiSkipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		ft, ok := aiFiletypeByExt[name]
		if !ok {
			ft, ok = aiFiletypeByExt[strings.ToLower(filepath.Ext(name))]
		}
		if ok {
			seen[ft] = true
		}
		return nil
	})
	out := make([]string, 0, len(seen))
	for ft := range seen {
		out = append(out, ft)
	}
	sort.Strings(out)
	return out
}

// ── lsp_servers / dap_adapters ───────────────────────────────────────────

// aiInstallKind is what one installer tool manages.
type aiInstallKind struct {
	tool  string // tool name
	cat   lspinstall.Category
	noun  string // "language server"
	after string // what happens after an install
}

var (
	aiLSPKind = aiInstallKind{"lsp_servers", lspinstall.CategoryLSP, "language server",
		"it starts for the open files of its language"}
	aiDAPKind = aiInstallKind{"dap_adapters", lspinstall.CategoryDAP, "debug adapter",
		"the debugger uses it from the next debug session"}
)

// lookup finds name in k's category.
func (k aiInstallKind) lookup(name string) (lspinstall.Tool, error) {
	t, ok := lspinstall.Lookup(name)
	if ok && t.Category == k.cat {
		return t, nil
	}
	var names []string
	for _, x := range lspinstall.ByCategory(k.cat) {
		names = append(names, x.Name)
	}
	return t, fmt.Errorf("no %s named %q; known: %s", k.noun, name, strings.Join(names, ", "))
}

// aiInstallSnap is the installer state seen from the tool goroutine.
type aiInstallSnap struct {
	installing bool
	progress   string
	result     *toolInstallResult
}

// aiInstallLook runs on Update: the state of one tool's install.
func (m *Model) aiInstallLook(name string) aiInstallSnap {
	m.ensureLSPMgr()
	s := aiInstallSnap{installing: m.lspMgr.installing[name], progress: m.lspMgr.progress[name]}
	if r, ok := m.lspMgr.results[name]; ok {
		s.result = &r
	}
	return s
}

// aiInstallState is the one-line state of t.
func aiInstallState(t lspinstall.Tool, snap aiInstallSnap) string {
	if snap.installing {
		return "installing… (" + snap.progress + ")"
	}
	st := lspinstall.StatusOf(t)
	var s string
	switch st.State {
	case lspinstall.Managed:
		s = "installed by termocode"
		if st.Version != "" {
			s += " (" + st.Version + ")"
		}
	case lspinstall.System:
		s = "on PATH (" + st.Path + "), not managed by termocode"
	default:
		s = "not installed"
		if err := toolInstaller().check(t); err != nil {
			s += "; can not install here: " + aiInstallErrText(err)
		}
	}
	if snap.result != nil && snap.result.err != nil {
		s += "; last install failed: " + aiInstallErrText(snap.result.err)
	}
	return s
}

// aiInstallErrText is a short, actionable text for an install error.
func aiInstallErrText(err error) string {
	var mt *lspinstall.MissingToolchainError
	if errors.As(err, &mt) {
		return mt.Bin + " is not on PATH (" + mt.Hint + ")"
	}
	var off *lspinstall.OfflineError
	if errors.As(err, &off) {
		return "offline: no network to download it"
	}
	return err.Error()
}

// aiInstallList is op list on the tool goroutine. all lists every tool.
func aiInstallList(ctx context.Context, call aiIDECaller, k aiInstallKind, root string, all bool) (string, error) {
	langs := aiWorkspaceFiletypes(root)
	use := map[string]bool{}
	for _, ft := range langs {
		use[ft] = true
	}
	snaps := map[string]aiInstallSnap{}
	tools := lspinstall.ByCategory(k.cat)
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		for _, t := range tools {
			snaps[t.Name] = m.aiInstallLook(t.Name)
		}
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	var b strings.Builder
	if len(langs) > 0 {
		fmt.Fprintf(&b, "Workspace languages: %s\n", strings.Join(langs, ", "))
	} else {
		b.WriteString("Workspace languages: none found\n")
	}
	shown := 0
	for _, t := range tools {
		forWS := false
		for _, ft := range t.Filetypes {
			forWS = forWS || use[ft]
		}
		snap := snaps[t.Name]
		if !all && !forWS && !snap.installing && lspinstall.StatusOf(t).State != lspinstall.Managed {
			continue
		}
		if shown == 0 {
			fmt.Fprintf(&b, "%ss:\n", strings.ToUpper(k.noun[:1])+k.noun[1:])
		}
		shown++
		fmt.Fprintf(&b, "- %s: %s [%s]\n", t.DisplayName(), aiInstallState(t, snap), strings.Join(t.Filetypes, ", "))
	}
	if shown == 0 {
		fmt.Fprintf(&b, "No %s for these languages; all: true lists every one termocode can install.\n", k.noun)
	}
	fmt.Fprintf(&b, "Install one with %s(op: \"install\", name). all: true lists all %d.", k.tool, len(tools))
	return b.String(), nil
}

// aiInstallTail is the last lines of an installer's output.
func aiInstallTail(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > aiInstallOutLines {
		lines = lines[len(lines)-aiInstallOutLines:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// aiInstallRun is op install on the tool goroutine: start it like the
// manager does (toasts, background), then wait up to wait.
func aiInstallRun(ctx context.Context, call aiIDECaller, k aiInstallKind, t lspinstall.Tool, wait time.Duration) (string, error) {
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		m.ensureLSPMgr()
		if m.lspMgr.installing[t.Name] {
			return "", nil, nil // already running: wait for it below
		}
		m.lspMgr.byAI[t.Name] = true
		return "", m.startToolInstall(t), nil
	}); err != nil {
		return "", err
	}
	var snap aiInstallSnap
	timedOut, err := aiPollUntil(ctx, wait, aiInstallPoll, func() (bool, error) {
		_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			snap = m.aiInstallLook(t.Name)
			return "", nil, nil
		})
		return !snap.installing, err
	})
	if err != nil {
		return "", err
	}
	if timedOut {
		return fmt.Sprintf("installing %s in the background (last step: %s). Check again with %s(op: \"list\").",
			t.Name, snap.progress, k.tool), nil
	}
	if snap.result == nil {
		return "", fmt.Errorf("the install of %s did not report a result; check with %s(op: \"list\")", t.Name, k.tool)
	}
	if snap.result.err != nil {
		msg := fmt.Sprintf("install of %s failed: %s", t.Name, aiInstallErrText(snap.result.err))
		if tail := aiInstallTail(snap.result.output); tail != "" {
			msg += "\nLast output:\n" + tail
		}
		return "", errors.New(msg)
	}
	st := lspinstall.StatusOf(t)
	return fmt.Sprintf("installed %s (%s); %s.", t.Name, st.Path, k.after), nil
}

// aiInstallRemove is op uninstall on the tool goroutine.
func aiInstallRemove(ctx context.Context, call aiIDECaller, t lspinstall.Tool) (string, error) {
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		m.ensureLSPMgr()
		if m.lspMgr.installing[t.Name] {
			return "", nil, fmt.Errorf("%s is installing right now; try again when it is done", t.Name)
		}
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	err := lspinstall.Uninstall(t)
	if _, cerr := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		return "", m.onToolUninstalled(lspUninstallDoneMsg{name: t.Name, err: err}), nil
	}); cerr != nil {
		return "", cerr
	}
	if err != nil {
		return "", fmt.Errorf("uninstall of %s failed: %w", t.Name, err)
	}
	out := "removed " + t.Name + " (termocode's copy); a running one stops when termocode restarts."
	if st := lspinstall.StatusOf(t); st.State == lspinstall.System {
		out += " A copy on PATH stays: " + st.Path
	}
	return out, nil
}

// aiInstallerTool is lsp_servers or dap_adapters.
func aiInstallerTool(k aiInstallKind, root string, call aiIDECaller) ai.Tool {
	var names []string
	for _, t := range lspinstall.ByCategory(k.cat) {
		names = append(names, t.Name)
	}
	return ai.Tool{
		Name: k.tool,
		Description: "Manage the " + k.noun + "s termocode installs (like the manager picker). op list: the " + k.noun +
			"s for the workspace's languages and their state (installed, on PATH, installing, failed). " +
			"op install: download and install one (asks the user; runs in the background, the tool waits up to " +
			"wait_ms, default 60000). op uninstall: remove termocode's copy (always asks). Known: " +
			strings.Join(names, ", ") + ".",
		Params: map[string]any{
			"op":      map[string]any{"type": "string", "enum": aiInstallOps, "description": "what to do"},
			"name":    map[string]any{"type": "string", "description": "install / uninstall: the " + k.noun + " name from list"},
			"all":     map[string]any{"type": "boolean", "description": "list: every " + k.noun + ", not only the workspace's"},
			"wait_ms": map[string]any{"type": "integer", "description": "install: wait up to this long (ms, max 120000; default 60000)"},
		},
		Required: []string{"op"},
		Mutating: true, Class: ai.ClassRun, // Gate decides per op
		Gate: func(a map[string]any) (bool, string) {
			switch ai.ArgString(a, "op") {
			case "install":
				return true, ai.ClassRun // downloads and runs code
			case "uninstall":
				return true, "" // deletes files: always ask
			}
			return false, ""
		},
		Validate: func(a map[string]any) error {
			op := ai.ArgString(a, "op")
			if err := aiOneOf("op", op, aiInstallOps); err != nil {
				return err
			}
			if op == "list" {
				return nil
			}
			name := strings.TrimSpace(ai.ArgString(a, "name"))
			if name == "" {
				return fmt.Errorf("%s needs name; see %s(op: \"list\")", op, k.tool)
			}
			t, err := k.lookup(name)
			if err != nil {
				return err
			}
			if op == "install" {
				if err := toolInstaller().check(t); err != nil {
					return fmt.Errorf("can not install %s here: %s", t.Name, aiInstallErrText(err))
				}
				return nil
			}
			switch st := lspinstall.StatusOf(t); st.State {
			case lspinstall.System:
				return fmt.Errorf("%s is on PATH (%s), not installed by termocode; termocode only removes its own copies", t.Name, st.Path)
			case lspinstall.Missing:
				return fmt.Errorf("%s is not installed", t.Name)
			}
			return nil
		},
		Describe: func(a map[string]any) string {
			switch ai.ArgString(a, "op") {
			case "install":
				return "Install " + k.noun + " " + ai.ArgString(a, "name") + " (downloads and runs its installer)"
			case "uninstall":
				return "Uninstall " + k.noun + " " + ai.ArgString(a, "name")
			}
			return "List " + k.noun + "s"
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			op := ai.ArgString(a, "op")
			if op == "list" || op == "" {
				return aiInstallList(ctx, call, k, root, ai.ArgBool(a, "all"))
			}
			t, err := k.lookup(strings.TrimSpace(ai.ArgString(a, "name")))
			if err != nil {
				return "", err
			}
			if op == "uninstall" {
				return aiInstallRemove(ctx, call, t)
			}
			wait := aiInstallWait
			if _, ok := a["wait_ms"]; ok {
				wait = aiWaitArg(a)
			}
			return aiInstallRun(ctx, call, k, t, wait)
		},
	}
}

// ── extensions ───────────────────────────────────────────────────────────

// aiExtList runs on Update: op list.
func (m *Model) aiExtList() string {
	var b strings.Builder
	dir, _ := ext.Dir()
	fmt.Fprintf(&b, "Extensions folder: %s\n", dir)
	if m.ext == nil {
		b.WriteString("Extensions are not available (no editor).")
		return b.String()
	}
	s := m.ext
	switch {
	case s.loading:
		b.WriteString("(loading…)\n")
	case s.loadErr != "":
		b.WriteString("Load failed: " + s.loadErr + "\n")
	}
	if len(s.reg.Extensions) == 0 {
		b.WriteString("No extensions loaded.\n")
	}
	for _, x := range s.reg.Extensions {
		state := "loaded"
		if !x.OK {
			state = "failed: " + x.Error
		}
		ver := ""
		if x.Version != "" {
			ver = " v" + x.Version
		}
		fmt.Fprintf(&b, "- %s%s: %s\n", x.Name, ver, state)
	}
	for _, x := range s.exts {
		if x.Problem != "" {
			fmt.Fprintf(&b, "- %s: problem: %s\n", x.Name, x.Problem)
		}
	}
	if len(s.reg.Commands) > 0 {
		b.WriteString("Commands (run with extensions(op: \"run\", id)):\n")
		for _, c := range s.reg.Commands {
			fmt.Fprintf(&b, "- %s: %s\n", c.ID, c.Title)
		}
	}
	for _, p := range s.reg.Panels {
		fmt.Fprintf(&b, "Panel %s: %s (%s)\n", p.ID, p.Title, p.Ext)
	}
	for _, st := range s.reg.Status {
		fmt.Fprintf(&b, "Status item %s (%s)\n", st.ID, st.Ext)
	}
	return strings.TrimRight(b.String(), "\n")
}

// aiExtRunLua runs a command and returns the log lines it added.
const aiExtRunLua = `local H = _G._termocode_ext
if not H then return '' end
local n = #H.log_lines
H.run(%s)
local out = {}
for i = n + 1, #H.log_lines do out[#out + 1] = H.log_lines[i] end
return table.concat(out, '\n')`

// aiExtRun is op run on the tool goroutine.
func aiExtRun(ctx context.Context, call aiIDECaller, id string) (string, error) {
	id = strings.TrimPrefix(id, extPaletteIDPfx)
	var title string
	var client func(code string) (string, error)
	var proc func() error // a process extension's command (N6b)
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if m.ext != nil && m.ext.proc != nil {
			if name, ok := m.ext.procCmd[id]; ok {
				for _, c := range m.ext.reg.Commands {
					if c.ID == id {
						title = c.Title
					}
				}
				h := m.ext.proc
				proc = func() error { return h.Execute(ctx, name, id, nil) }
				return "", nil, nil
			}
		}
		if m.ext == nil || m.nvim == nil {
			return "", nil, errors.New("extensions are not available (no editor)")
		}
		if !m.ext.loaded {
			return "", nil, errors.New("extensions are not loaded; see extensions(op: \"list\")")
		}
		for _, c := range m.ext.reg.Commands {
			if c.ID == id {
				title = c.Title
			}
		}
		if title == "" {
			return "", nil, fmt.Errorf("no extension command %q; extensions(op: \"list\") shows the ids", id)
		}
		c := m.nvim
		client = func(code string) (string, error) { return extEval(c, code, extCallTimeout) }
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	if proc != nil {
		if err := proc(); err != nil {
			return "", fmt.Errorf("extension command %s: %w", id, err)
		}
		return fmt.Sprintf("ran %s (%s)", id, title), nil
	}
	logged, err := client(fmt.Sprintf(aiExtRunLua, luaQuote(id)))
	// The command may have changed panels or status items.
	if _, cerr := call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.extPollCmd(), nil }); cerr != nil {
		return "", cerr
	}
	if err != nil {
		return "", fmt.Errorf("extension command %s: %w", id, err)
	}
	logged = sanitizeExtText(logged)
	if strings.Contains(logged, "ERROR ") {
		return "", fmt.Errorf("extension command %s failed:\n%s", id, logged)
	}
	out := fmt.Sprintf("ran %s (%s)", id, title)
	if logged != "" {
		out += "; it logged:\n" + logged
	}
	return out, nil
}

// aiExtReload is op reload on the tool goroutine: reload and wait for
// the load to finish, then list.
func aiExtReload(ctx context.Context, call aiIDECaller) (string, error) {
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if m.ext == nil || m.nvim == nil {
			return "", nil, errors.New("extensions are not available (no editor)")
		}
		return "", m.extReload(), nil
	}); err != nil {
		return "", err
	}
	var out string
	timedOut, err := aiPollUntil(ctx, aiExtReloadWait, aiInstallPoll, func() (bool, error) {
		done := false
		_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			done = !m.ext.loading
			if done {
				out = m.aiExtList()
			}
			return "", nil, nil
		})
		return done, err
	})
	if err != nil {
		return "", err
	}
	if timedOut {
		return "", errors.New("the extensions did not finish loading in time; see extensions(op: \"log\")")
	}
	return "reloaded.\n" + out, nil
}

// aiExtLog is op log on the tool goroutine: load results and the last
// lines of the extension log.
func aiExtLog(ctx context.Context, call aiIDECaller, lines int) (string, error) {
	var head string
	var eval func(string) (string, error)
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		head = m.aiExtList()
		if m.nvim != nil {
			c := m.nvim
			eval = func(code string) (string, error) { return extEval(c, code, extCallTimeout) }
		}
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	if eval == nil {
		return head, nil
	}
	log, err := eval("local H = _G._termocode_ext; return H and H.log_text() or ''")
	if err != nil {
		return head + "\n(log not available: " + err.Error() + ")", nil
	}
	if lines <= 0 {
		lines = aiExtLogLines
	}
	all := strings.Split(strings.TrimRight(sanitizeExtText(log), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	if strings.TrimSpace(strings.Join(all, "")) == "" {
		return head + "\nLog: empty", nil
	}
	return head + "\nLog:\n" + strings.Join(all, "\n"), nil
}

func aiExtensionsTool(call aiIDECaller) ai.Tool {
	return ai.Tool{
		Name: "extensions",
		Description: "termocode's Lua extensions (~/.config/termocode/extensions). op list: each extension (loaded or " +
			"the error), its commands, panels and status items. op run: run an extension command by id (asks). " +
			"op reload: reload all extensions from disk, e.g. after you edit one (asks). op log: load results and " +
			"the extension log (errors, termocode.log lines).",
		Params: map[string]any{
			"op":    map[string]any{"type": "string", "enum": aiExtOps, "description": "what to do"},
			"id":    map[string]any{"type": "string", "description": "run: the command id from list, e.g. todo-tree.refresh"},
			"lines": map[string]any{"type": "integer", "description": "log: how many last log lines (default 100)"},
		},
		Required: []string{"op"},
		Mutating: true, Class: ai.ClassRun, // Gate decides per op
		Gate: func(a map[string]any) (bool, string) {
			switch ai.ArgString(a, "op") {
			case "run", "reload":
				return true, ai.ClassRun // runs extension code
			}
			return false, ""
		},
		Validate: func(a map[string]any) error {
			op := ai.ArgString(a, "op")
			if err := aiOneOf("op", op, aiExtOps); err != nil {
				return err
			}
			if op == "run" && strings.TrimSpace(ai.ArgString(a, "id")) == "" {
				return errors.New("run needs id; extensions(op: \"list\") shows the command ids")
			}
			return nil
		},
		Describe: func(a map[string]any) string {
			switch ai.ArgString(a, "op") {
			case "run":
				return "Run extension command " + ai.ArgString(a, "id")
			case "reload":
				return "Reload extensions (runs their code)"
			case "log":
				return "Read the extension log"
			}
			return "List extensions"
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			switch ai.ArgString(a, "op") {
			case "run":
				return aiExtRun(ctx, call, strings.TrimSpace(ai.ArgString(a, "id")))
			case "reload":
				return aiExtReload(ctx, call)
			case "log":
				return aiExtLog(ctx, call, ai.ArgInt(a, "lines"))
			}
			return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiExtList(), nil, nil })
		},
	}
}

// ── workspace_folders ────────────────────────────────────────────────────

// aiNoOpenWorkspace is why the AI never opens another workspace.
const aiNoOpenWorkspace = "opening another workspace restarts termocode in that folder and would end this chat; " +
	"use workspace_folders(op: \"add\") to show a folder next to this one, or ask the user to open it " +
	"(File: Open Workspace)"

// aiFolderList runs on Update: op list.
func (m *Model) aiFolderList() string {
	primary := m.explorer.PrimaryRoot()
	roots := m.explorer.Roots()
	var b strings.Builder
	fmt.Fprintf(&b, "%d workspace folders:\n", len(roots))
	for _, r := range roots {
		if r == primary {
			fmt.Fprintf(&b, "- %s (primary; file tools, git and the terminal work here)\n", r)
		} else {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// aiFolderRemove runs on Update: op remove. path is an absolute path,
// a path relative to the workspace root, or the folder's name.
func (m *Model) aiFolderRemove(root, path string) (string, tea.Cmd, error) {
	primary := m.explorer.PrimaryRoot()
	target := ""
	if p, err := aiFolderPath(root, path); err == nil {
		for _, r := range m.explorer.Roots() {
			if filepath.Clean(r) == p {
				target = r
			}
		}
	}
	if target == "" { // by name, when only one root has it
		n := 0
		for _, r := range m.explorer.Roots() {
			if filepath.Base(r) == path {
				target = r
				n++
			}
		}
		if n > 1 {
			return "", nil, fmt.Errorf("more than one workspace folder is named %q; pass the full path", path)
		}
	}
	if target == "" {
		return "", nil, fmt.Errorf("%q is not a workspace folder; workspace_folders(op: \"list\") shows them", path)
	}
	if target == primary {
		return "", nil, errors.New("the primary folder can not be removed (it is the workspace itself)")
	}
	cmd := m.removeRootFromPicker(target)
	return "removed " + target + " from the workspace; the explorer no longer shows it.", cmd, nil
}

// aiFolderPath resolves path: "~", relative to root, cleaned.
func aiFolderPath(root, path string) (string, error) {
	p, err := expandUser(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return filepath.Clean(p), nil
}

func aiWorkspaceFoldersTool(root string, call aiIDECaller) ai.Tool {
	return ai.Tool{
		Name: "workspace_folders",
		Description: "The folders shown in the explorer (multi-root workspace). op list: every folder; the first is " +
			"the primary one (file tools, git and the terminal work there). op add: show another folder as a new " +
			"root in the explorer (asks). op remove: take an added folder out again (asks). Opening a different " +
			"workspace is not possible: it restarts termocode and ends this chat.",
		Params: map[string]any{
			"op":   map[string]any{"type": "string", "enum": aiFolderOps, "description": "what to do"},
			"path": map[string]any{"type": "string", "description": "add / remove: folder path (absolute, ~/…, or relative to the workspace root); remove also takes the folder name"},
		},
		Required: []string{"op"},
		Mutating: true, Class: ai.ClassIDE, // Gate decides per op
		Gate: func(a map[string]any) (bool, string) {
			switch ai.ArgString(a, "op") {
			case "add", "remove":
				return true, ai.ClassIDE
			}
			return false, "" // list; open is refused by Validate
		},
		Validate: func(a map[string]any) error {
			op := ai.ArgString(a, "op")
			if err := aiOneOf("op", op, aiFolderOps); err != nil {
				return err
			}
			switch op {
			case "open":
				return errors.New(aiNoOpenWorkspace)
			case "add", "remove":
				if strings.TrimSpace(ai.ArgString(a, "path")) == "" {
					return fmt.Errorf("%s needs path", op)
				}
			}
			if op == "add" {
				p, err := aiFolderPath(root, ai.ArgString(a, "path"))
				if err != nil {
					return err
				}
				info, err := os.Stat(p)
				if err != nil {
					return fmt.Errorf("can not add %s: %w", p, err)
				}
				if !info.IsDir() {
					return fmt.Errorf("can not add %s: not a folder", p)
				}
			}
			return nil
		},
		Describe: func(a map[string]any) string {
			switch ai.ArgString(a, "op") {
			case "add":
				return "Add folder to the workspace: " + ai.ArgString(a, "path")
			case "remove":
				return "Remove folder from the workspace: " + ai.ArgString(a, "path")
			}
			return "List workspace folders"
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			path := strings.TrimSpace(ai.ArgString(a, "path"))
			switch ai.ArgString(a, "op") {
			case "open":
				return "", errors.New(aiNoOpenWorkspace)
			case "add":
				p, err := aiFolderPath(root, path)
				if err != nil {
					return "", err
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					for _, r := range m.explorer.Roots() {
						if filepath.Clean(r) == p {
							return p + " is already in the workspace", nil, nil
						}
					}
					if err := m.explorer.AddRoot(p); err != nil {
						return "", nil, err
					}
					m.persistWorkspaceRoots()
					cmd := m.pushToastDetail(toast.Info, "Added", filepath.Base(p))
					return "added " + p + " to the workspace; the explorer shows it as a new root. " +
						"read_file / write_file work only in the primary folder; use run_command for this one.", cmd, nil
				})
			case "remove":
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiFolderRemove(root, path) })
			}
			return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiFolderList(), nil, nil })
		},
	}
}

// aiAdminTools are the K8 tools.
func aiAdminTools(root string, call aiIDECaller) []ai.Tool {
	return []ai.Tool{
		aiInstallerTool(aiLSPKind, root, call),
		aiInstallerTool(aiDAPKind, root, call),
		aiExtensionsTool(call),
		aiWorkspaceFoldersTool(root, call),
	}
}
