package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/toast"
)

// AI tools that drive the IDE itself: open a file, read the editor state,
// show a panel, run a safe palette command. Tools run on the agent
// goroutine and must not touch Model fields, so each one sends an
// aiIDECallMsg over the chat channel; Update runs fn on the real Model
// and replies. The tool waits for the reply or for ctx (Stop / New Chat),
// so a cancel never hangs.

// aiIDEFunc runs on the Update goroutine. The cmd (may be nil) is
// returned from Update.
type aiIDEFunc func(m *Model) (string, tea.Cmd, error)

type aiIDEReply struct {
	out string
	err error
}

type aiIDECallMsg struct {
	gen   uint64
	ctx   context.Context
	fn    aiIDEFunc
	reply chan aiIDEReply // buffered (1): Update never blocks on it
	ch    chan tea.Msg
}

// aiIDECaller sends fn to Update and waits for its result.
type aiIDECaller func(ctx context.Context, fn aiIDEFunc) (string, error)

var errAIChatStopped = errors.New("the chat was stopped")

// newAIIDECaller builds the caller for one chat turn (gen, ch).
func newAIIDECaller(gen uint64, ch chan tea.Msg) aiIDECaller {
	return func(ctx context.Context, fn aiIDEFunc) (string, error) {
		reply := make(chan aiIDEReply, 1)
		select {
		case ch <- aiIDECallMsg{gen: gen, ctx: ctx, fn: fn, reply: reply, ch: ch}:
		case <-ctx.Done():
			return "", errAIChatStopped
		}
		select {
		case r := <-reply:
			return r.out, r.err
		case <-ctx.Done():
			return "", errAIChatStopped
		}
	}
}

// onAIIDECall runs one IDE call on the real Model. A call from an old
// chat (New Chat, history pick) or a stopped one is not run.
func (m *Model) onAIIDECall(msg aiIDECallMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen != s.chatGen || (msg.ctx != nil && msg.ctx.Err() != nil) {
		msg.reply <- aiIDEReply{err: errAIChatStopped}
		return waitAIChat(msg.ch)
	}
	out, cmd, err := msg.fn(m)
	msg.reply <- aiIDEReply{out: out, err: err}
	return tea.Batch(cmd, waitAIChat(msg.ch))
}

// aiActionTitle is the palette title of a built-in id ("" when none).
func aiActionTitle(id string) string {
	for _, it := range append(paletteItems(), tasksPaletteItems()...) {
		if it.ID == id {
			return it.Title
		}
	}
	return ""
}

// aiCheckAction refuses ids execute_action must not run (ai_actions_policy.go).
func aiCheckAction(id string) (aiActionPolicy, error) {
	p, ok := aiActionPolicyFor(id)
	if !ok {
		return p, fmt.Errorf("%q is not an IDE command id; find ids with list_actions(query)", id)
	}
	if p.Gate == aiGateNever {
		return p, fmt.Errorf("command %q is not allowed for the AI: %s", id, p.Reason)
	}
	return p, nil
}

// aiLiveActions are the palette rows of this session: built-ins, tasks,
// user commands and extension commands.
func (m *Model) aiLiveActions() []picker.Item {
	items := append(paletteItems(), tasksPaletteItems()...)
	items = append(items, userCommandsItems()...)
	return append(items, m.extPaletteItems()...)
}

// aiPanelNames are the show_panel targets.
var aiPanelNames = []string{"problems", "output", "terminal", "tests", "test-results", "explorer", "git", "search", "http-response"}

// aiIDETools are the tools that act on the IDE through call.
func aiIDETools(root string, call aiIDECaller) []ai.Tool {
	ws := ai.Workspace{Root: root}
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	rel := aiRel(root)
	// existing resolves a workspace path that must exist.
	existing := func(a map[string]any) (string, os.FileInfo, error) {
		p := ai.ArgString(a, "path")
		if p == "" {
			return "", nil, errors.New("path is required")
		}
		abs, err := ws.Resolve(p)
		if err != nil {
			return "", nil, err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return "", nil, fmt.Errorf("%s does not exist (create it first with create_file)", p)
		}
		return abs, fi, nil
	}
	return []ai.Tool{
		{
			Name:        "open_file",
			Description: "Open a file in the user's editor (as the active tab), optionally at a line and column, and reveal it in the explorer.",
			Params:      map[string]any{"path": str("file path"), "line": num("1-based line (optional)"), "column": num("1-based column (optional)")},
			Required:    []string{"path"},
			Describe:    func(a map[string]any) string { return "Open " + ai.ArgString(a, "path") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				abs, fi, err := existing(a)
				if err != nil {
					return "", err
				}
				if fi.IsDir() {
					return "", fmt.Errorf("%s is a folder; use reveal_in_explorer", ai.ArgString(a, "path"))
				}
				line, col := ai.ArgInt(a, "line"), ai.ArgInt(a, "column")
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					return m.aiOpenFile(abs, rel(abs), line, col)
				})
			},
		},
		{
			Name:        "get_editor_state",
			Description: "Read the editor state: active file, cursor, selection, visible lines, open tabs, unsaved files and focus.",
			Describe:    func(map[string]any) string { return "Read editor state" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					out, err := m.aiEditorState(rel)
					return out, nil, err
				})
			},
		},
		{
			Name:        "notify",
			Description: "Show the user a short notification (toast) in the IDE.",
			Params:      map[string]any{"title": str("short message"), "detail": str("second line (optional)")},
			Required:    []string{"title"},
			Describe:    func(a map[string]any) string { return "Notify: " + ai.ArgString(a, "title") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				title, detail := ai.ArgString(a, "title"), ai.ArgString(a, "detail")
				if strings.TrimSpace(title) == "" {
					return "", errors.New("title is required")
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					var cmd tea.Cmd
					m.toast, cmd = m.toast.PushDetail(toast.Info, title, detail)
					return "shown", cmd, nil
				})
			},
		},
		{
			Name:        "show_panel",
			Description: "Show an IDE panel: " + strings.Join(aiPanelNames, ", ") + ".",
			Params: map[string]any{"name": map[string]any{"type": "string", "enum": aiPanelNames,
				"description": "panel to show"}},
			Required: []string{"name"},
			Describe: func(a map[string]any) string { return "Show " + ai.ArgString(a, "name") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				name := ai.ArgString(a, "name")
				if !contains(aiPanelNames, name) {
					return "", fmt.Errorf("unknown panel %q; use one of %s", name, strings.Join(aiPanelNames, ", "))
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					return "shown " + name, m.aiShowPanel(name), nil
				})
			},
		},
		{
			Name:        "reveal_in_explorer",
			Description: "Show a file or folder in the explorer tree (does not open it).",
			Params:      map[string]any{"path": str("file or folder path")},
			Required:    []string{"path"},
			Describe:    func(a map[string]any) string { return "Reveal " + ai.ArgString(a, "path") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				abs, _, err := existing(a)
				if err != nil {
					return "", err
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					m.aiShowSidebar(activity.ViewFiles)
					m.explorer.RevealPath(abs)
					return "revealed " + rel(abs), nil, nil
				})
			},
		},
		{
			Name: "list_actions",
			Description: "Find IDE commands (the command palette) for execute_action. Returns id, title, category " +
				"and gate (auto runs at once, confirm asks the user). Every query word must match the id or title.",
			Params:   map[string]any{"query": str("words to match, e.g. \"fold\" or \"git stage\"; empty lists all")},
			Describe: func(a map[string]any) string { return "List IDE commands " + ai.ArgString(a, "query") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				q := ai.ArgString(a, "query")
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					return aiListActions(m.aiLiveActions(), q, aiListActionsMax), nil, nil
				})
			},
		},
		{
			Name: "execute_action",
			Description: "Run an IDE command (command palette) by id; find ids with list_actions. " +
				"Safe commands run at once, others ask the user. Commands act on the active file / cursor.",
			Params:   map[string]any{"id": str("command id")},
			Required: []string{"id"},
			Mutating: true, Class: ai.ClassIDE, // Gate decides per id
			Describe: func(a map[string]any) string {
				id := ai.ArgString(a, "id")
				if t := aiActionTitle(id); t != "" {
					return "IDE: " + t
				}
				return "IDE command " + id
			},
			Gate: func(a map[string]any) (bool, string) {
				p, err := aiCheckAction(ai.ArgString(a, "id"))
				if err != nil || p.Gate != aiGateAuto {
					return true, p.Class
				}
				return false, ""
			},
			Validate: func(a map[string]any) error {
				_, err := aiCheckAction(ai.ArgString(a, "id"))
				return err
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				id := ai.ArgString(a, "id")
				if _, err := aiCheckAction(id); err != nil { // Validate ran first; keep Run safe alone
					return "", err
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					return "ran " + id, m.dispatchPaletteAction(id), nil
				})
			},
		},
	}
}

// aiOpenFile opens abs in the editor (images in the preview), focuses the
// editor and reveals the file in the explorer.
func (m *Model) aiOpenFile(abs, rel string, line, col int) (string, tea.Cmd, error) {
	if termimg.IsImagePath(abs) {
		return "opened image preview of " + rel, m.openImagePreview(abs), nil
	}
	if !isSupportedFile(abs) {
		t, d := unsupportedFileMessage(abs)
		return "", nil, errors.New(t + ": " + d)
	}
	if m.nvim == nil {
		return "", nil, errors.New("the editor is not running")
	}
	m.openFileAtLine(abs, line)
	if col > 0 {
		_ = m.nvim.ExecLuaArgs(`local c = ... ; local p = vim.api.nvim_win_get_cursor(0)
			pcall(vim.api.nvim_win_set_cursor, 0, { p[1], math.max(c - 1, 0) })`, col)
	}
	name, err := m.nvim.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`)
	if err != nil || filepath.Clean(name) != abs {
		return "", nil, fmt.Errorf("could not open %s in the editor", rel)
	}
	m.focus = FocusEditor
	m.inTerminal = false
	m.panelFocused = false
	m.aiS().focused = false
	m.explorer.RevealPath(abs)
	out := "opened " + rel
	if line > 0 {
		out += fmt.Sprintf(" at line %d", line)
	}
	return out, nil, nil
}

// aiShowSidebar shows sidebar view v, as an activity-bar click does.
func (m *Model) aiShowSidebar(v activity.View) tea.Cmd {
	m.activity.SetActive(v)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	if v == activity.ViewTests {
		return m.ensureTestsDiscovered()
	}
	return nil
}

// aiShowPanel shows one show_panel target.
func (m *Model) aiShowPanel(name string) tea.Cmd {
	switch name {
	case "problems":
		return m.showProblemsPanel()
	case "output":
		m.showPanelTab(panelKindOutput)
	case "terminal":
		m.showPanelTab(panelKindTerminal)
	case "test-results":
		m.showPanelTab(panelKindTests)
	case "tests":
		return m.aiShowSidebar(activity.ViewTests)
	case "explorer":
		return m.aiShowSidebar(activity.ViewFiles)
	case "git":
		return m.aiShowSidebar(activity.ViewGit)
	case "search":
		return m.aiShowSidebar(activity.ViewSearch)
	case "http-response": // the HTTP client's page (http_page.go)
		return m.httpShowLast()
	}
	return nil
}

// aiFocusName names where keys go now.
func (m *Model) aiFocusName() string {
	switch {
	case m.ai != nil && m.ai.focused && m.actionsPanelVisible():
		return "ai-chat"
	case m.inTerminal:
		return "terminal"
	case m.panelFocused:
		return "panel"
	case m.focus == FocusExplorer:
		return "explorer"
	}
	return "editor"
}

const aiMaxSelection = 4000

// aiEditorStateLua reads the editor window (never the terminal / panel
// split) without moving focus.
const aiEditorStateLua = `
local maxsel = ...
local function editorwin(w)
  local b = vim.api.nvim_win_get_buf(w)
  return vim.bo[b].buftype == '' and not vim.w[w].termocode_panel
end
local win = vim.api.nvim_get_current_win()
if not editorwin(win) then
  for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if editorwin(w) then win = w break end
  end
end
local buf = vim.api.nvim_win_get_buf(win)
local cur = vim.api.nvim_win_get_cursor(win)
local st = {
  file = vim.api.nvim_buf_get_name(buf), line = cur[1], column = cur[2] + 1,
  line_count = vim.api.nvim_buf_line_count(buf), filetype = vim.bo[buf].filetype,
  mode = vim.api.nvim_get_mode().mode,
}
vim.api.nvim_win_call(win, function()
  st.visible_first = vim.fn.line('w0')
  st.visible_last = vim.fn.line('w$')
  local m = vim.fn.mode()
  if m == 'v' or m == 'V' or m == '\22' then
    local s, e = vim.fn.getpos('v'), vim.fn.getpos('.')
    local ok, lines = pcall(vim.fn.getregion, s, e, { type = m })
    if not ok then lines = {} end
    local text = table.concat(lines, '\n')
    local truncated = #text > maxsel
    if truncated then text = text:sub(1, maxsel) end
    local a, z = s, e
    if a[2] > z[2] or (a[2] == z[2] and a[3] > z[3]) then a, z = z, a end
    st.selection = { start_line = a[2], start_column = a[3], end_line = z[2], end_column = z[3], text = text, truncated = truncated }
  end
end)
return vim.json.encode(st)
`

// aiEditorState is the get_editor_state result (JSON).
func (m *Model) aiEditorState(rel func(string) string) (string, error) {
	st := map[string]any{"focus": m.aiFocusName()}
	if m.nvim == nil {
		st["file"] = ""
		b, _ := json.Marshal(st)
		return string(b), nil
	}
	var raw string
	if err := m.nvim.ExecLuaResult(aiEditorStateLua, &raw, aiMaxSelection); err != nil {
		return "", err
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return "", err
	}
	st["focus"] = m.aiFocusName()
	if f, _ := st["file"].(string); f != "" {
		st["file"] = rel(f)
	}
	tabs, modified := []string{}, []string{}
	if bufs, _, err := m.nvim.Buffers(); err == nil {
		for _, b := range bufs {
			if b.Path == "" || strings.Contains(b.Path, "://") { // terminals, plugin buffers
				continue
			}
			tabs = append(tabs, rel(b.Path))
			if b.Modified {
				modified = append(modified, rel(b.Path))
			}
		}
	}
	st["tabs"], st["modified"] = tabs, modified
	b, err := json.Marshal(st)
	return string(b), err
}
