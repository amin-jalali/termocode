package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"termocode/internal/picker"
	"termocode/internal/tasks"
	"termocode/internal/toast"
)

// ── Tasks (Group C) ──────────────────────────────────────────────────────
//
// "Run Task…" lists .termocode/tasks.json tasks, the user's commands.json
// entries and auto-detected ones (internal/tasks). A task runs in its own
// PTY terminal tab (the integrated terminal); the tab is named after the
// task and its status dot turns green / red on exit. When the task has a
// problemMatcher, its output is scanned on exit and the problems land in
// nvim via vim.diagnostic.set (namespace "termocode-tasks") — so they show
// in the gutter and in the Problems panel.
//
// Three ways in: Alt+R (run) / Alt+Shift+R (rerun), the palette
// ("Tasks: …"), and the overflow menu / a click on a task tab.

// tasksNamespace is the vim.diagnostic namespace for task problems.
const tasksNamespace = "termocode-tasks"

// tasksOutputChannel is the Output channel with the run log.
const tasksOutputChannel = "tasks"

// taskRun is one started task.
type taskRun struct {
	Task    tasks.Task
	Buf     int
	Dir     string
	Started time.Time
	Running bool
}

// taskRunner holds the task state. Shared by pointer.
type taskRunner struct {
	last  *tasks.Task
	runs  map[int]*taskRun // by terminal buffer id
	order []int            // buffer ids, oldest first
}

func newTaskRunner() *taskRunner {
	return &taskRunner{runs: map[int]*taskRun{}}
}

// ensureTasks returns m.tasks, creating it for bare Model literals.
func (m *Model) ensureTasks() *taskRunner {
	if m.tasks == nil {
		m.tasks = newTaskRunner()
	}
	return m.tasks
}

// taskVars returns the ${…} values for the current workspace / file.
func (m Model) taskVars() tasks.Vars {
	root, _ := os.Getwd()
	file := m.editor.Path()
	if file != "" && !filepath.IsAbs(file) {
		file = filepath.Join(root, file)
	}
	return tasks.Vars{WorkspaceFolder: root, File: file}
}

// userCommandTasks turns commands.json entries into tasks (Source "user").
func userCommandTasks() []tasks.Task {
	var out []tasks.Task
	for _, c := range loadUserCommands() {
		if c.Cmd == "" {
			continue
		}
		title := c.Title
		if title == "" {
			title = c.ID
		}
		out = append(out, tasks.Task{
			Label:   "User: " + title,
			Command: c.Cmd,
			Cwd:     c.CWD,
			Source:  "user",
		})
	}
	return out
}

// allTasks returns configured, user and detected tasks (first label wins).
// err is a tasks.json parse error; the other sources still load.
func (m Model) allTasks() ([]tasks.Task, error) {
	root := m.taskVars().WorkspaceFolder
	configured, err := tasks.Load(root)
	return tasks.Merge(configured, userCommandTasks(), tasks.Detect(root)), err
}

// toastTasksError reports a broken tasks.json (once per call site).
func (m *Model) toastTasksError(err error) tea.Cmd {
	if err == nil {
		return nil
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Errr, "tasks.json is invalid", err.Error())
	return c
}

// openTaskPicker shows every task ("Tasks: Run Task…", Alt+R).
func (m *Model) openTaskPicker() tea.Cmd {
	list, err := m.allTasks()
	errCmd := m.toastTasksError(err)
	if len(list) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "No tasks found",
			"Run \"Tasks: Configure Tasks\" to create .termocode/tasks.json")
		return tea.Batch(errCmd, c)
	}
	v := m.taskVars()
	items := make([]picker.Item, 0, len(list))
	for _, t := range list {
		hint := t.Source
		if t.Group != "" {
			hint += " · " + t.Group
		}
		items = append(items, picker.Item{
			ID:    "task:" + t.Label,
			Title: t.Label,
			Hint:  truncateHint(hint+" · "+t.CommandLine(v), 48),
		})
	}
	m.openTasksPickerItems(" Run Task ", items)
	return errCmd
}

func (m *Model) openTasksPickerItems(title string, items []picker.Item) {
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindTasks
}

// handleTasksPickerSelect runs the picked task or opens the picked link.
func (m *Model) handleTasksPickerSelect(id string) tea.Cmd {
	switch {
	case strings.HasPrefix(id, "task:"):
		return m.runTaskByLabel(strings.TrimPrefix(id, "task:"))
	case strings.HasPrefix(id, "link:"):
		// link:<line>:<col>:<abs path>
		var line, col int
		rest := strings.TrimPrefix(id, "link:")
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) == 3 {
			fmt.Sscanf(parts[0], "%d", &line)
			fmt.Sscanf(parts[1], "%d", &col)
			m.jumpToDiagnostic(parts[2], line, col)
		}
	}
	return nil
}

// runTaskByLabel looks a task up by label and runs it.
func (m *Model) runTaskByLabel(label string) tea.Cmd {
	list, err := m.allTasks()
	t, ok := tasks.Find(list, label)
	if !ok {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "Task not found", label)
		return tea.Batch(m.toastTasksError(err), c)
	}
	return m.runTask(t)
}

// runTaskGroup runs the default build / test task. With no test task
// configured, the test group falls back to the classic test runner
// (test_runner.go — Group E owns its future).
func (m *Model) runTaskGroup(group string) tea.Cmd {
	list, err := m.allTasks()
	errCmd := m.toastTasksError(err)
	if group == tasks.GroupTest {
		if d, ok := tasks.DefaultFor(onlySource(list, "tasks.json"), tasks.GroupTest); ok {
			return tea.Batch(errCmd, m.runTask(d))
		}
		return tea.Batch(errCmd, m.runTestsCmd())
	}
	if t, ok := tasks.DefaultFor(list, group); ok {
		return tea.Batch(errCmd, m.runTask(t))
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "No "+group+" task found",
		"Add one with \"group\": \""+group+"\" in .termocode/tasks.json")
	return tea.Batch(errCmd, c)
}

func onlySource(list []tasks.Task, src string) []tasks.Task {
	var out []tasks.Task
	for _, t := range list {
		if t.Source == src {
			out = append(out, t)
		}
	}
	return out
}

// rerunLastTask runs the last task again (Alt+Shift+R).
func (m *Model) rerunLastTask() tea.Cmd {
	tr := m.ensureTasks()
	if tr.last == nil {
		return m.openTaskPicker()
	}
	return m.runTask(*tr.last)
}

// running returns the running tasks, newest first.
func (tr *taskRunner) running() []*taskRun {
	var out []*taskRun
	for i := len(tr.order) - 1; i >= 0; i-- {
		if r := tr.runs[tr.order[i]]; r != nil && r.Running {
			out = append(out, r)
		}
	}
	return out
}

// terminateTask stops the task in the active terminal tab, else the
// newest running task.
func (m *Model) terminateTask() tea.Cmd {
	tr := m.ensureTasks()
	running := tr.running()
	var target *taskRun
	if tab := m.activeTerminalTab(); tab != nil && m.panelActive == panelKindTerminal {
		if r := tr.runs[tab.BufID]; r != nil && r.Running {
			target = r
		}
	}
	if target == nil && len(running) > 0 {
		target = running[0]
	}
	if target == nil {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No task is running")
		return c
	}
	if m.nvim != nil {
		_ = m.nvim.ExecLua(fmt.Sprintf(`
			local b = %d
			if vim.api.nvim_buf_is_loaded(b) then
				local ok, chan = pcall(function() return vim.bo[b].channel end)
				if ok and chan and chan > 0 then pcall(vim.fn.jobstop, chan) end
			end
		`, target.Buf))
	}
	return nil
}

// configureTasks opens .termocode/tasks.json, seeding it with the detected
// tasks (or a template) when it does not exist yet.
func (m *Model) configureTasks() tea.Cmd {
	root := m.taskVars().WorkspaceFolder
	path := tasks.ConfigPath(root)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		body := tasks.Template
		if det := tasks.Detect(root); len(det) > 0 {
			body = tasks.Marshal(det)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = os.WriteFile(path, []byte(body), 0o644)
		}
		if err != nil {
			var c tea.Cmd
			m.toast, c = m.toast.PushDetail(toast.Errr, "Cannot create tasks.json", err.Error())
			return c
		}
	}
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`vim.cmd('edit ' .. vim.fn.fnameescape(...))`, path)
	}
	m.focus = FocusEditor
	return nil
}

// ── Running in a PTY terminal tab ────────────────────────────────────────

// taskSpawnLua opens a terminal buffer in the panel window running cmd.
// Autocmds are suppressed so TermOpen / BufEnter do not drop the editor
// into terminal-insert; focus stays where it was. on_exit reports the
// exit code to Go (termocode_task_exit) and keeps the buffer so the
// output stays readable.
const taskSpawnLua = `
	local termWin, cmd, cwd, env, label = ...
	if not vim.api.nvim_win_is_valid(termWin) then return 0 end
	local buf = vim.api.nvim_create_buf(false, true)
	if buf <= 0 then return 0 end
	vim.bo[buf].buflisted = false
	vim.bo[buf].swapfile = false
	local prev = vim.api.nvim_get_current_win()
	local ei = vim.o.eventignore
	vim.o.eventignore = 'all'
	pcall(vim.api.nvim_win_set_buf, termWin, buf)
	pcall(vim.api.nvim_set_current_win, termWin)
	local opts = {
		cwd = cwd,
		on_exit = function(_, code)
			vim.schedule(function()
				if _G.termocode_notify then _G.termocode_notify('termocode_task_exit', buf, code) end
			end)
		end,
	}
	if type(env) == 'table' and next(env) ~= nil then opts.env = env end
	local argv = { vim.o.shell, '-c', cmd }
	local ok, job
	if vim.fn.has('nvim-0.11') == 1 then
		opts.term = true
		ok, job = pcall(vim.fn.jobstart, argv, opts)
	else
		ok, job = pcall(vim.fn.termopen, argv, opts)
	end
	if prev ~= termWin and vim.api.nvim_win_is_valid(prev) then
		pcall(vim.api.nvim_set_current_win, prev)
	end
	vim.o.eventignore = ei
	if not ok or not job or job <= 0 then
		pcall(vim.api.nvim_buf_delete, buf, { force = true })
		return 0
	end
	vim.b[buf].termocode_task = label
	pcall(function() vim.wo[termWin].number = false end)
	pcall(function() vim.wo[termWin].relativenumber = false end)
	pcall(function() vim.wo[termWin].signcolumn = 'no' end)
	pcall(function() vim.wo[termWin].foldcolumn = '0' end)
	pcall(function() vim.wo[termWin].statuscolumn = '' end)
	pcall(function() vim.wo[termWin].cursorline = false end)
	return buf
`

// runTask starts t in a new terminal tab. A finished tab of the same task
// is replaced; a running one is stopped first (restart).
func (m *Model) runTask(t tasks.Task) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	tr := m.ensureTasks()
	v := m.taskVars()
	cmdline := t.CommandLine(v)
	dir := t.Dir(v)
	env := map[string]any{}
	for k, val := range t.Env {
		env[k] = tasks.Expand(val, v)
	}
	if !m.termOpen || m.terminalWinID <= 0 {
		if !m.ensurePanelHost(true) {
			return nil
		}
	}
	var bufNum int64
	err := m.nvim.EvalLuaArgs(taskSpawnLua, &bufNum, m.terminalWinID, cmdline, dir, env, t.Label)
	if err != nil || bufNum <= 0 {
		var c tea.Cmd
		detail := cmdline
		if err != nil {
			detail = err.Error()
		}
		m.toast, c = m.toast.PushDetail(toast.Errr, "Task failed to start", detail)
		return c
	}
	buf := int(bufNum)
	// Old tab(s) of the same task go away (the new one replaces them).
	for i := len(m.terminalTabs) - 1; i >= 0; i-- {
		if m.terminalTabs[i].Task == t.Label {
			old := m.terminalTabs[i].BufID
			m.terminalTabs = append(m.terminalTabs[:i:i], m.terminalTabs[i+1:]...)
			_ = m.nvim.ExecLua(fmt.Sprintf(`
				local b = %d
				if vim.api.nvim_buf_is_loaded(b) then
					local ok, chan = pcall(function() return vim.bo[b].channel end)
					if ok and chan and chan > 0 then pcall(vim.fn.jobstop, chan) end
					pcall(vim.api.nvim_buf_delete, b, { force = true })
				end
			`, old))
			delete(tr.runs, old)
		}
	}
	m.terminalTabs = append(m.terminalTabs, terminalTab{
		BufID:    buf,
		Cwd:      dir,
		Name:     t.Label,
		Shell:    detectShellBasename(),
		LastExit: -1,
		Task:     t.Label,
		Running:  true,
	})
	m.terminalActiveTab = len(m.terminalTabs) - 1
	m.panelActive = panelKindTerminal
	m.panelFocused = false
	m.terminalMinimized = false
	m.inTerminal = false
	m.focus = FocusEditor
	m.applyLayout()
	m.resizeTerminalSplit()
	m.invalidateTerminalProbeCache()

	tt := t
	tr.last = &tt
	tr.runs[buf] = &taskRun{Task: t, Buf: buf, Dir: dir, Started: time.Now(), Running: true}
	tr.order = append(tr.order, buf)
	m.output.Append(tasksOutputChannel, fmt.Sprintf("▶ %s: %s  (in %s)", t.Label, cmdline, dir))
	return nil
}

// onTaskExit handles termocode_task_exit(buf, code).
func (m *Model) onTaskExit(buf, code int) tea.Cmd {
	tr := m.ensureTasks()
	for i := range m.terminalTabs {
		if m.terminalTabs[i].BufID == buf {
			m.terminalTabs[i].LastExit = code
			m.terminalTabs[i].Running = false
		}
	}
	r := tr.runs[buf]
	if r == nil || !r.Running {
		return nil
	}
	r.Running = false
	took := time.Since(r.Started).Round(100 * time.Millisecond)
	mark := "✓"
	if code != 0 {
		mark = "✘"
	}
	m.output.Append(tasksOutputChannel, fmt.Sprintf("%s %s exited with code %d (%s)", mark, r.Task.Label, code, took))

	var cmds []tea.Cmd
	if code != 0 {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr,
			fmt.Sprintf("Task \"%s\" failed", r.Task.Label), fmt.Sprintf("exit code %d", code))
		cmds = append(cmds, c)
	}
	if tasks.Enabled(r.Task.ProblemMatcher) && m.nvim != nil {
		cmds = append(cmds, m.matchTaskProblemsCmd(*r))
	}
	return tea.Batch(cmds...)
}

// matchTaskProblemsCmd reads the task's terminal output, runs the problem
// matchers and publishes the result as diagnostics.
func (m Model) matchTaskProblemsCmd(r taskRun) tea.Cmd {
	c := m.nvim
	root := m.taskVars().WorkspaceFolder
	return func() tea.Msg {
		var lines []string
		if err := c.EvalLuaArgs(`
			local b = ...
			if not vim.api.nvim_buf_is_loaded(b) then return {} end
			return vim.api.nvim_buf_get_lines(b, 0, -1, false)
		`, &lines, r.Buf); err != nil {
			return nil
		}
		probs := tasks.Match(r.Task.ProblemMatcher, lines)
		res := newPathResolver(root)
		groups := map[string][]any{}
		var order []string
		for _, p := range probs {
			abs := res.resolve(p.Path, r.Dir)
			if abs == "" {
				continue
			}
			if _, ok := groups[abs]; !ok {
				order = append(order, abs)
			}
			groups[abs] = append(groups[abs], []any{p.Line, p.Col, p.Severity, p.Message})
		}
		payload := make([]any, 0, len(order))
		for _, path := range order {
			payload = append(payload, []any{path, groups[path]})
		}
		_ = c.ExecLuaArgs(setTaskDiagnosticsLua, payload, "task: "+r.Task.Label, tasksNamespace)
		n := 0
		for _, g := range groups {
			n += len(g)
		}
		label := r.Task.Label
		return applyMsg(func(m *Model) tea.Cmd {
			m.output.Append(tasksOutputChannel, fmt.Sprintf("  %s: %d problem(s) found", label, n))
			return nil
		})
	}
}

// setTaskDiagnosticsLua replaces this task's diagnostics (by source) in
// the shared namespace, leaving other tasks' problems alone. Buffers are
// created with bufadd so files that are not open still get problems.
const setTaskDiagnosticsLua = `
	local groups, src, nsname = ...
	local ns = vim.api.nvim_create_namespace(nsname)
	local by = {}
	for _, d in ipairs(vim.diagnostic.get(nil, { namespace = ns })) do
		by[d.bufnr] = by[d.bufnr] or {}
		if d.source ~= src then table.insert(by[d.bufnr], d) end
	end
	for _, g in ipairs(groups) do
		local b = vim.fn.bufadd(g[1])
		by[b] = by[b] or {}
		for _, d in ipairs(g[2]) do
			table.insert(by[b], {
				lnum = math.max(d[1] - 1, 0), col = math.max(d[2] - 1, 0),
				severity = d[3], message = d[4], source = src,
			})
		end
	end
	for b, list in pairs(by) do
		if vim.api.nvim_buf_is_valid(b) then pcall(vim.diagnostic.set, ns, b, list) end
	end
`

// clearTaskProblems drops every task diagnostic.
func (m *Model) clearTaskProblems() tea.Cmd {
	if m.nvim != nil {
		_ = m.nvim.ExecLuaArgs(`pcall(vim.diagnostic.reset, vim.api.nvim_create_namespace(...))`, tasksNamespace)
	}
	return nil
}

// ── Path resolution for matched problems / links ────────────────────────

// pathResolver turns printed paths into existing absolute files. It tries
// the task dir, then the workspace root, then a by-suffix search of the
// workspace (built lazily, once).
type pathResolver struct {
	root  string
	index map[string][]string // basename → absolute paths
}

func newPathResolver(root string) *pathResolver { return &pathResolver{root: root} }

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// resolve returns the absolute path for printed, or "" when not found.
func (r *pathResolver) resolve(printed, dir string) string {
	if filepath.IsAbs(printed) {
		if fileExists(printed) {
			return filepath.Clean(printed)
		}
		return ""
	}
	for _, base := range []string{dir, r.root} {
		if base == "" {
			continue
		}
		if p := tasks.ResolvePath(printed, base); fileExists(p) {
			return p
		}
	}
	if r.index == nil {
		r.buildIndex()
	}
	want := filepath.Clean(strings.TrimPrefix(printed, "./"))
	cands := r.index[filepath.Base(want)]
	sort.Strings(cands)
	for _, c := range cands {
		if strings.HasSuffix(c, string(filepath.Separator)+want) {
			return c
		}
	}
	return ""
}

// skipDirs are never searched for problem files.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "target": true, ".termocode": true}

func (r *pathResolver) buildIndex() {
	r.index = map[string][]string{}
	if r.root == "" {
		return
	}
	n := 0
	_ = filepath.WalkDir(r.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != r.root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		n++
		if n > 50000 {
			return filepath.SkipAll
		}
		r.index[d.Name()] = append(r.index[d.Name()], p)
		return nil
	})
}

// ── Clickable file:line links in terminal tabs ──────────────────────────

// taskTabGlyph is the tab-bar icon of a task tab: ▶ running, ✓ / ✘ done.
func taskTabGlyph(t terminalTab) string {
	switch {
	case t.Running:
		return "▶"
	case t.LastExit == 0:
		return "✓"
	case t.LastExit > 0:
		return "✘"
	}
	return "▶"
}

// cellToByte maps a display column to a byte offset in s (-1 when past
// the end). Pure for tests.
func cellToByte(s string, cell int) int {
	col := 0
	for i, r := range s {
		w := runewidth.RuneWidth(r)
		if cell < col+w || (w == 0 && cell == col) {
			return i
		}
		col += w
	}
	return -1
}

// routeTerminalLinkClick opens a file:line link under a left click in a
// terminal tab. Returns false (click not consumed) when there is no link
// under the pointer.
func (m *Model) routeTerminalLinkClick(msg tea.MouseMsg) bool {
	if msg.Type != tea.MouseLeft || !m.termOpen || m.terminalMinimized ||
		m.panelActive != panelKindTerminal || m.nvim == nil {
		return false
	}
	tab := m.activeTerminalTab()
	if tab == nil || m.terminalWinID <= 0 {
		return false
	}
	bar := m.terminalTabBarRowAbsolute()
	paneW := m.editorPaneWidth()
	x0 := m.w - paneW
	if bar < 0 || msg.Y <= bar || msg.Y >= m.h-1 || msg.X < x0 {
		return false
	}
	row, cell := msg.Y-bar-1, msg.X-x0
	var text string
	if err := m.nvim.EvalLuaArgs(`
		local win, row = ...
		if not vim.api.nvim_win_is_valid(win) then return '' end
		local top = vim.fn.line('w0', win)
		local buf = vim.api.nvim_win_get_buf(win)
		return vim.api.nvim_buf_get_lines(buf, top - 1 + row, top + row, false)[1] or ''
	`, &text, m.terminalWinID, row); err != nil || text == "" {
		return false
	}
	off := cellToByte(text, cell)
	if off < 0 {
		return false
	}
	link, ok := tasks.LinkAt(text, off)
	if !ok {
		return false
	}
	abs := newPathResolver(m.taskVars().WorkspaceFolder).resolve(link.Path, tab.Cwd)
	if abs == "" {
		return false
	}
	m.jumpToDiagnostic(abs, link.Line, link.Col)
	return true
}

// openTerminalLinksPicker lists every file:line link in the active
// terminal tab ("Terminal: Open Link…").
func (m *Model) openTerminalLinksPicker() tea.Cmd {
	tab := m.activeTerminalTab()
	if tab == nil || m.nvim == nil {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No terminal tab is open")
		return c
	}
	var lines []string
	_ = m.nvim.EvalLuaArgs(`
		local b = ...
		if not vim.api.nvim_buf_is_loaded(b) then return {} end
		local n = vim.api.nvim_buf_line_count(b)
		return vim.api.nvim_buf_get_lines(b, math.max(n - 2000, 0), -1, false)
	`, &lines, tab.BufID)
	res := newPathResolver(m.taskVars().WorkspaceFolder)
	seen := map[string]bool{}
	var items []picker.Item
	for i := len(lines) - 1; i >= 0; i-- {
		for _, l := range tasks.ParseLinks(lines[i]) {
			abs := res.resolve(l.Path, tab.Cwd)
			id := fmt.Sprintf("link:%d:%d:%s", l.Line, l.Col, abs)
			if abs == "" || seen[id] {
				continue
			}
			seen[id] = true
			items = append(items, picker.Item{
				ID:    id,
				Title: fmt.Sprintf("%s:%d", problemRelPath(abs), l.Line),
				Hint:  truncateHint(strings.TrimSpace(tasks.StripANSI(lines[i])), 48),
			})
		}
	}
	if len(items) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No file links in this terminal")
		return c
	}
	m.openTasksPickerItems(" Open Link ", items)
	return nil
}

// ── Palette / keys ───────────────────────────────────────────────────────

// tasksPaletteItems are the Group C palette rows.
func tasksPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "tasks-run", Title: "Tasks: Run Task...", Hint: "Alt+R"},
		{ID: "tasks-build", Title: "Tasks: Run Build Task"},
		{ID: "tasks-test", Title: "Tasks: Run Test Task"},
		{ID: "tasks-rerun", Title: "Tasks: Rerun Last Task", Hint: "Alt+Shift+R"},
		{ID: "tasks-terminate", Title: "Tasks: Terminate Task"},
		{ID: "tasks-configure", Title: "Tasks: Configure Tasks"},
		{ID: "tasks-clear-problems", Title: "Tasks: Clear Task Problems"},
		{ID: "terminal-open-link", Title: "Terminal: Open File Link..."},
		{ID: "problems-panel", Title: "View: Problems", Hint: "Ctrl+Shift+M / Alt+M"},
	}
}

// dispatchTasksPalette handles the Group C palette IDs.
func (m *Model) dispatchTasksPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "tasks-run":
		return m.openTaskPicker(), true
	case "tasks-build":
		return m.runTaskGroup(tasks.GroupBuild), true
	case "tasks-test":
		return m.runTaskGroup(tasks.GroupTest), true
	case "tasks-rerun":
		return m.rerunLastTask(), true
	case "tasks-terminate":
		return m.terminateTask(), true
	case "tasks-configure":
		return m.configureTasks(), true
	case "tasks-clear-problems":
		return m.clearTaskProblems(), true
	case "terminal-open-link":
		return m.openTerminalLinksPicker(), true
	case "problems-panel":
		return m.showProblemsPanel(), true
	}
	return nil, false
}

func init() {
	registerNotifyHandler("termocode_task_exit", func(m *Model, args []any) tea.Cmd {
		return m.onTaskExit(notifyArgInt(args, 0), notifyArgInt(args, 1))
	})
}
