package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Dev run configs (CTX-37):
//
//   - a "▸ build" lens on each FROM line of a Dockerfile and a "▸ up" lens
//     on each compose service; a click on it, or Containers: Run at
//     Cursor, shows a Cancel-first card with the command, then runs it
//     in a terminal tab;
//   - Containers: Add Docker Files… writes a starter Dockerfile,
//     compose.yaml and .dockerignore from a template (built-in or the
//     user's own folder under the config dir); existing files are kept;
//   - Containers: Add Compose Service… puts a service from the catalog
//     into the workspace compose file.

// devrunPending is the job or files a Containers card waits for.
type devrunPending struct {
	cmd     services.Command
	tab     string
	plan    containers.ScaffoldPlan
	compose string // the compose file to write
	text    string // its new text
}

// Card operations (runHostAction).
const (
	hostOpRunCommand  = "run-command"
	hostOpScaffold    = "scaffold"
	hostOpComposeSvc  = "compose-service"
	devrunTemplateDir = "docker-templates"
)

func init() {
	registerNotifyHandler("termocode_docker_buf", func(m *Model, args []any) tea.Cmd {
		m.dockerLensRefresh(notifyArgInt(args, 0))
		return nil
	})
	registerNotifyHandler("termocode_docker_lens", func(m *Model, args []any) tea.Cmd {
		name, lines, ok := m.httpBufText(notifyArgInt(args, 0))
		if !ok {
			return nil
		}
		return m.askRunTarget(name, lines, notifyArgInt(args, 1), true)
	})
}

// dockerLensLua draws the run lens from a Go-made row table
// (_G._termocode_docker_bufs[buf] = { [lnum] = { lens = chunks } }) and
// sends a click on it to Go. Buffers of other files cost nothing.
const dockerLensLua = `
local api = vim.api
_G._termocode_docker_bufs = _G._termocode_docker_bufs or {}
local B = _G._termocode_docker_bufs
local ns = api.nvim_create_namespace('termocode_docker')

function _G._termocode_docker_buf_set(buf, rows)
  if not api.nvim_buf_is_valid(buf) then return end
  local t = {}
  for _, r in ipairs(rows) do t[r.l] = r end
  B[buf] = (#rows > 0) and t or nil
  pcall(api.nvim__buf_redraw_range, buf, 0, -1)
end

local function width(s) return vim.fn.strdisplaywidth(s) end
local function lens_width(r)
  local w = 0
  for _, c in ipairs(r.lens or {}) do w = w + width(c[1]) end
  return w
end

api.nvim_set_decoration_provider(ns, {
  on_win = function(_, _, b) return B[b] ~= nil end,
  on_line = function(_, win, b, row)
    local r = B[b] and B[b][row + 1]
    if not r then return end
    local info = vim.fn.getwininfo(win)[1]
    local text_w = api.nvim_win_get_width(win) - ((info and info.textoff) or 0)
    local line = api.nvim_buf_get_lines(b, row, row + 1, false)[1] or ''
    if width(line) + lens_width(r) + 1 <= text_w then
      pcall(api.nvim_buf_set_extmark, b, ns, row, 0, { virt_text = r.lens, virt_text_pos = 'right_align', ephemeral = true })
    end
  end,
})

local timers = {}
local function changed(buf)
  if timers[buf] then timers[buf]:stop() end
  timers[buf] = vim.defer_fn(function()
    timers[buf] = nil
    if api.nvim_buf_is_valid(buf) and _G.termocode_notify then _G.termocode_notify('termocode_docker_buf', buf) end
  end, 150)
end

local function lens_hit(buf)
  local pos = vim.fn.getmousepos()
  if pos.winid == 0 or api.nvim_win_get_buf(pos.winid) ~= buf or pos.line <= 0 then return nil end
  local r = B[buf] and B[buf][pos.line]
  if not r then return nil end
  local ww = api.nvim_win_get_width(pos.winid)
  if pos.wincol > ww - lens_width(r) and pos.wincol <= ww then return pos.line end
end

local patterns = { 'Dockerfile', 'Dockerfile.*', '*.Dockerfile', '*.dockerfile', 'Containerfile',
  'compose.yaml', 'compose.yml', 'compose.*.yaml', 'compose.*.yml',
  'docker-compose.yaml', 'docker-compose.yml', 'docker-compose.*.yaml', 'docker-compose.*.yml' }
api.nvim_create_augroup('termocode_docker', { clear = true })
api.nvim_create_autocmd({ 'BufReadPost', 'BufNewFile' }, {
  group = 'termocode_docker',
  pattern = patterns,
  callback = function(args)
    local buf = args.buf
    if vim.b[buf].termocode_docker_attached then return end
    vim.b[buf].termocode_docker_attached = true
    api.nvim_buf_attach(buf, false, { on_lines = function() changed(buf) end })
    api.nvim_create_autocmd('BufEnter', { group = 'termocode_docker', buffer = buf, callback = function() changed(buf) end })
    api.nvim_create_autocmd('BufWipeout', { group = 'termocode_docker', buffer = buf, callback = function() B[buf] = nil end })
    vim.keymap.set({ 'n', 'i' }, '<LeftMouse>', function()
      local line = lens_hit(buf)
      if line then
        vim.schedule(function() if _G.termocode_notify then _G.termocode_notify('termocode_docker_lens', buf, line) end end)
        return '<Ignore>'
      end
      return '<LeftMouse>'
    end, { buffer = buf, expr = true, silent = true })
    changed(buf)
  end,
})
`

// dockerLensRows is the decoration table of one file.
func dockerLensRows(name string, lines []string) []map[string]any {
	rows := []map[string]any{}
	for _, t := range containers.RunTargets(name, lines) {
		rows = append(rows, map[string]any{"l": t.Line, "lens": [][]string{{" " + runTargetLens(t) + " ", "TermocodeHttpLens"}}})
	}
	return rows
}

// runTargetLens is the lens text of t.
func runTargetLens(t containers.RunTarget) string {
	switch {
	case t.Kind == containers.RunService:
		return "▸ up " + t.Service
	case t.Stage != "" && !t.Last:
		return "▸ build to " + t.Stage
	}
	return "▸ build"
}

// dockerLensRefresh rebuilds the lens of one buffer.
func (m *Model) dockerLensRefresh(buf int) {
	if m.nvim == nil {
		return
	}
	name, lines, ok := m.httpBufText(buf)
	if !ok {
		return
	}
	if err := m.nvim.ExecLuaArgs(`local b, r = ...
if _G._termocode_docker_buf_set then _G._termocode_docker_buf_set(b, r) end`, buf, dockerLensRows(name, lines)); err != nil {
		recordError(fmt.Sprintf("docker lens: %v", err))
	}
}

// runAtCursor is Containers: Run at Cursor.
func (m *Model) runAtCursor() tea.Cmd {
	if m.nvim == nil {
		return m.servicesToast(toast.Info, "Open a Dockerfile or compose file first", "")
	}
	name, lines, ok := m.httpBufText(0)
	if !ok {
		return m.servicesToast(toast.Info, "Open a Dockerfile or compose file first", "")
	}
	line := 1
	if c, err := m.nvim.CursorContext(); err == nil && c.Line > 0 {
		line = c.Line
	}
	return m.askRunTarget(name, lines, line, false)
}

// askRunTarget shows the card of the target at line (exact: only a mark
// on that very line, as for a click).
func (m *Model) askRunTarget(name string, lines []string, line int, exact bool) tea.Cmd {
	ts := containers.RunTargets(name, lines)
	if ts == nil {
		return m.servicesToast(toast.Info, "Not a Dockerfile or compose file", "Run at Cursor works in a Dockerfile (FROM lines) and in compose.yaml (services).")
	}
	pick := containers.RunTargetAt
	if exact {
		pick = containers.RunTargetOn
	}
	t, ok := pick(ts, line)
	if !ok {
		if exact {
			return nil
		}
		t = ts[0]
	}
	e := m.services.containerEngine
	if e.Binary == "" {
		return m.servicesToast(toast.Info, "No container engine found", "Install or start Docker or Podman, then try again.")
	}
	cmd := e.RunTargetCommand(name, t, defaultBuildTag(name))
	title := "Build image?"
	button := "Build"
	if t.Kind == containers.RunService {
		title, button = "Start service "+t.Service+"?", "Start"
	}
	return m.askContainerCommand(title, "Review the command before it runs. It shows its output in a terminal tab.", button, cmd, containerTabPrefix+"run:"+name+":"+fmt.Sprint(t.Line))
}

// askContainerCommand is the Cancel-first card before a visible job.
func (m *Model) askContainerCommand(title, body, button string, cmd services.Command, tab string) tea.Cmd {
	m.services.devrun = devrunPending{cmd: cmd, tab: tab}
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = cmd.Argv, hostOpRunCommand, ""
	m.confirm = confirm.New(title, body+"\n\nCommand: "+svcShellLine(cmd.Argv), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "run", Title: button, Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	return nil
}

// runDevrunHostAction runs a confirmed devrun card; false for other
// operations.
func (m *Model) runDevrunHostAction(operation string) (tea.Cmd, bool) {
	p := m.services.devrun
	m.services.devrun = devrunPending{}
	switch operation {
	case hostOpRunCommand:
		if err := m.servicesRunCommand(p.cmd, p.tab); err != nil {
			return m.servicesToast(toast.Errr, "Cannot start the command", err.Error()), true
		}
		return nil, true
	case hostOpScaffold:
		wrote, err := containers.WriteScaffold(p.plan)
		if err != nil {
			return m.servicesToast(toast.Errr, "Cannot write the docker files", err.Error()), true
		}
		if len(wrote) == 0 {
			return m.servicesToast(toast.Info, "Nothing written", "All files already exist."), true
		}
		for _, f := range p.plan.Files {
			if f.Rel == "Dockerfile" && !f.Exists {
				m.openTestLocation(f.Path, 1)
			}
		}
		return m.servicesToast(toast.Info, "Added "+strings.Join(wrote, ", "), "Click ▸ build on a FROM line to build."), true
	case hostOpComposeSvc:
		if err := os.WriteFile(p.compose, []byte(p.text), 0o644); err != nil {
			return m.servicesToast(toast.Errr, "Cannot write the compose file", err.Error()), true
		}
		m.openTestLocation(p.compose, 1)
		return m.servicesToast(toast.Info, "Service added", filepath.Base(p.compose)), true
	}
	return nil, false
}

// devrunTemplates is the catalog: the detected built-in first, then the
// other built-ins, then the user's own.
func devrunTemplates(root string) ([]containers.Template, string) {
	all := containers.BuiltinTemplates()
	det := containers.DetectTemplate(root)
	out := make([]containers.Template, 0, len(all))
	for _, t := range all {
		if t.ID == det {
			out = append([]containers.Template{t}, out...)
		} else {
			out = append(out, t)
		}
	}
	note := ""
	if dir, err := configDir(); err == nil {
		custom, err := containers.LoadCustomTemplates(filepath.Join(dir, devrunTemplateDir))
		if err != nil {
			note = err.Error()
		}
		out = append(out, custom...)
	}
	return out, note
}

// askScaffold is Containers: Add Docker Files…: the template picker.
func (m *Model) askScaffold() tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		return m.servicesToast(toast.Errr, "Cannot find the workspace folder", err.Error())
	}
	ts, note := devrunTemplates(root)
	det := containers.DetectTemplate(root)
	items := make([]picker.Item, 0, len(ts))
	for _, t := range ts {
		hint := ""
		if t.ID == det {
			hint = "fits this folder"
		} else if t.Custom {
			hint = "your template"
		}
		items = append(items, picker.Item{ID: "ctr:scaffold:" + t.ID, Title: t.Title, Hint: hint})
	}
	m.openTasksPickerItems(" Add Docker Files ", items)
	if note != "" {
		return m.servicesToast(toast.Warn, "Some of your templates could not be read", note)
	}
	return nil
}

// askScaffoldTemplate shows the card of one template: what is created,
// what is kept.
func (m *Model) askScaffoldTemplate(id string) tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		return m.servicesToast(toast.Errr, "Cannot find the workspace folder", err.Error())
	}
	ts, _ := devrunTemplates(root)
	for _, t := range ts {
		if t.ID != id {
			continue
		}
		plan := containers.PlanScaffold(root, t)
		create := plan.Create()
		if len(create) == 0 {
			return m.servicesToast(toast.Info, "These files already exist", "Nothing to add: existing files are never overwritten.")
		}
		body := "Creates in " + root + ":\n"
		for _, f := range create {
			body += "  + " + f.Rel + "\n"
		}
		if kept := plan.Kept(); len(kept) > 0 {
			body += "Kept as they are:\n"
			for _, f := range kept {
				body += "  = " + f.Rel + "\n"
			}
		}
		m.services.devrun = devrunPending{plan: plan}
		m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = nil, hostOpScaffold, ""
		m.confirm = confirm.New("Add docker files ("+t.Title+")?", strings.TrimRight(body, "\n"), []confirm.Button{
			{ID: "cancel", Title: "Cancel"}, {ID: "create", Title: "Create", Style: confirm.StylePrimary},
		})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
		return nil
	}
	return m.servicesToast(toast.Info, "Template not found", id)
}

// workspaceComposeFile is the compose file of root ("" path = none yet;
// compose.yaml is then created).
func workspaceComposeFile(root string) (string, bool) {
	for _, n := range services.ComposeFileNames {
		if p := filepath.Join(root, n); fileExists(p) {
			return p, true
		}
	}
	return filepath.Join(root, "compose.yaml"), false
}

// askComposeSnippet is Containers: Add Compose Service…: the catalog.
func (m *Model) askComposeSnippet() tea.Cmd {
	items := []picker.Item{}
	for _, s := range containers.ComposeSnippets() {
		items = append(items, picker.Item{ID: "ctr:snippet:" + s.ID, Title: s.Title, Hint: s.ID})
	}
	m.openTasksPickerItems(" Add Compose Service ", items)
	return nil
}

// askComposeSnippetAdd shows the card that adds one service.
func (m *Model) askComposeSnippetAdd(id string) tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		return m.servicesToast(toast.Errr, "Cannot find the workspace folder", err.Error())
	}
	var s containers.ComposeSnippet
	for _, c := range containers.ComposeSnippets() {
		if c.ID == id {
			s = c
		}
	}
	if s.ID == "" {
		return m.servicesToast(toast.Info, "Service template not found", id)
	}
	path, exists := workspaceComposeFile(root)
	old := ""
	if exists {
		b, err := os.ReadFile(path)
		if err != nil {
			return m.servicesToast(toast.Errr, "Cannot read the compose file", err.Error())
		}
		old = string(b)
	}
	text, err := containers.AddComposeService(old, s)
	if err != nil {
		return m.servicesToast(toast.Info, "Service not added", err.Error())
	}
	verb := "Adds to "
	if !exists {
		verb = "Creates "
	}
	m.services.devrun = devrunPending{compose: path, text: text}
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = nil, hostOpComposeSvc, ""
	m.confirm = confirm.New("Add service "+s.ID+"?", verb+path+":\n\n"+strings.TrimRight(s.Body, "\n"), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "add", Title: "Add", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	return nil
}

// handleContainersPickerSelect runs a picked "ctr:<what>:<id>".
func (m *Model) handleContainersPickerSelect(id string) tea.Cmd {
	what, rest, _ := strings.Cut(strings.TrimPrefix(id, "ctr:"), ":")
	switch what {
	case "scaffold":
		return m.askScaffoldTemplate(rest)
	case "snippet":
		return m.askComposeSnippetAdd(rest)
	case "custom": // CTX-38 (containers_custom.go)
		return m.pickCustomCommand(rest)
	}
	return nil
}

// devrunPaletteItems are the CTX-37 palette rows.
func devrunPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "containers-run-at-cursor", Title: "Containers: Run Dockerfile Stage or Compose Service at Cursor"},
		{ID: "containers-scaffold", Title: "Containers: Add Docker Files…"},
		{ID: "containers-compose-snippet", Title: "Containers: Add Compose Service…"},
		{ID: "containers-templates-folder", Title: "Containers: Open Docker Templates Folder"},
	}
}

// openTemplatesFolder makes the user's template folder (with a README
// template) and reveals it in the editor.
func (m *Model) openTemplatesFolder() tea.Cmd {
	dir, err := configDir()
	if err == nil {
		err = os.MkdirAll(filepath.Join(dir, devrunTemplateDir), 0o755)
	}
	if err != nil {
		return m.servicesToast(toast.Errr, "Config error", err.Error())
	}
	return m.editConfigFile(filepath.Join(devrunTemplateDir, "README.md"), templatesReadme)
}

const templatesReadme = `# Your docker templates

Each folder here is one template for "Containers: Add Docker Files…".
Its files (Dockerfile, compose.yaml, .dockerignore, …) are copied into
the workspace as they are. Files that already exist are never
overwritten. Sub-folders and links inside a template are skipped.

    docker-templates/
      my-service/
        Dockerfile
        compose.yaml
`
