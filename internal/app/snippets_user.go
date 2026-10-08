package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// User snippets live in ~/.config/termocode/snippets.json:
//
//	{
//	  "*":  { "todo": "// TODO(${1:me}): $0" },
//	  "go": { "errw": ["if err != nil {", "\treturn fmt.Errorf(\"${1:ctx}: %w\", err)", "}"] }
//	}
//
// Top-level keys are scopes: a Neovim filetype ("go", "python",
// "typescriptreact", …) or "*" for every filetype ("global" / "all" are
// accepted as aliases). Each scope maps trigger -> body; a body is a
// string or an array of lines. Bodies use LSP snippet syntax (${1:x},
// $0). At lookup time the Lua side layers them over the bundled table
// (snippets_lua.go): bundled < user "*" < user filetype. So a user
// snippet always wins on a trigger clash.

const userSnippetsFile = "snippets.json"

// globalSnippetScope is the scope key for snippets that apply everywhere.
const globalSnippetScope = "*"

// userSnippets is scope -> trigger -> body.
type userSnippets map[string]map[string]string

// snippetRef points at one user snippet.
type snippetRef struct {
	Scope   string
	Trigger string
}

// userSnippet is one flattened row for the manager picker.
type userSnippet struct {
	Scope   string
	Trigger string
	Body    string
}

// normalizeSnippetScope lowercases a scope and folds the global aliases
// into "*".
func normalizeSnippetScope(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "*", "global", "all":
		return globalSnippetScope
	}
	return s
}

// validSnippetTrigger mirrors the Lua matcher, which walks back over
// [%w_] characters before the cursor: anything else could never expand.
func validSnippetTrigger(t string) bool {
	if t == "" {
		return false
	}
	for _, r := range t {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// parseScopeTrigger splits the "New Snippet" prompt input "go:iferr".
// A bare trigger ("iferr") means the global scope.
func parseScopeTrigger(s string) (scope, trigger string, err error) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		scope, trigger = s[:i], s[i+1:]
	} else {
		trigger = s
	}
	scope = normalizeSnippetScope(scope)
	trigger = strings.TrimSpace(trigger)
	if !validSnippetTrigger(trigger) {
		return "", "", fmt.Errorf("trigger %q must be letters, digits or _", trigger)
	}
	if strings.ContainsAny(scope, " \t\"") {
		return "", "", fmt.Errorf("bad scope %q", scope)
	}
	return scope, trigger, nil
}

// unescapeBodyInput turns the one-line prompt input into a real body:
// `\n` -> newline, `\t` -> tab, `\\` -> backslash. Other escapes (like
// `\$`, which snippet syntax needs) pass through untouched.
func unescapeBodyInput(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 't':
				b.WriteByte('\t')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseUserSnippets decodes snippets.json. Invalid entries (non-string
// bodies, bad triggers) are skipped; only malformed JSON is an error.
func parseUserSnippets(data []byte) (userSnippets, error) {
	out := userSnippets{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return out, nil
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return out, err
	}
	for scope, items := range raw {
		sc := normalizeSnippetScope(scope)
		for trig, rawBody := range items {
			if !validSnippetTrigger(trig) {
				continue
			}
			var body string
			if err := json.Unmarshal(rawBody, &body); err != nil {
				var lines []string
				if err := json.Unmarshal(rawBody, &lines); err != nil {
					continue
				}
				body = strings.Join(lines, "\n")
			}
			out.set(sc, trig, body)
		}
	}
	return out, nil
}

// marshal encodes the snippets with sorted keys; multi-line bodies are
// written as arrays of lines so the file stays easy to hand-edit.
func (u userSnippets) marshal() []byte {
	enc := map[string]map[string]any{}
	for scope, items := range u {
		if len(items) == 0 {
			continue
		}
		m := map[string]any{}
		for trig, body := range items {
			if strings.Contains(body, "\n") {
				m[trig] = strings.Split(body, "\n")
			} else {
				m[trig] = body
			}
		}
		enc[scope] = m
	}
	b, err := json.MarshalIndent(enc, "", "  ")
	if err != nil {
		return []byte("{}\n")
	}
	return append(b, '\n')
}

// set adds or replaces one snippet. Returns true when it replaced one.
func (u userSnippets) set(scope, trigger, body string) bool {
	scope = normalizeSnippetScope(scope)
	if u[scope] == nil {
		u[scope] = map[string]string{}
	}
	_, existed := u[scope][trigger]
	u[scope][trigger] = body
	return existed
}

// remove deletes one snippet. Returns false if it was not there.
func (u userSnippets) remove(ref snippetRef) bool {
	scope := normalizeSnippetScope(ref.Scope)
	items, ok := u[scope]
	if !ok {
		return false
	}
	if _, ok := items[ref.Trigger]; !ok {
		return false
	}
	delete(items, ref.Trigger)
	if len(items) == 0 {
		delete(u, scope)
	}
	return true
}

// scopes returns every scope that has snippets, "*" first then A–Z.
func (u userSnippets) scopes() []string {
	out := make([]string, 0, len(u))
	for s, items := range u {
		if len(items) > 0 {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return scopeLess(out[i], out[j]) })
	return out
}

func scopeLess(a, b string) bool {
	if a == globalSnippetScope || b == globalSnippetScope {
		return a == globalSnippetScope && b != globalSnippetScope
	}
	return a < b
}

// list flattens the snippets for the manager. filter "" means every
// scope; otherwise only that scope.
func (u userSnippets) list(filter string) []userSnippet {
	var out []userSnippet
	for _, scope := range u.scopes() {
		if filter != "" && normalizeSnippetScope(filter) != scope {
			continue
		}
		for trig, body := range u[scope] {
			out = append(out, userSnippet{Scope: scope, Trigger: trig, Body: body})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return scopeLess(out[i].Scope, out[j].Scope)
		}
		return out[i].Trigger < out[j].Trigger
	})
	return out
}

// nextSnippetScope cycles the manager's scope filter: all -> each scope
// -> all.
func nextSnippetScope(scopes []string, cur string) string {
	if cur == "" {
		if len(scopes) == 0 {
			return ""
		}
		return scopes[0]
	}
	for i, s := range scopes {
		if s == cur && i+1 < len(scopes) {
			return scopes[i+1]
		}
	}
	return ""
}

// luaLongString quotes s as a Lua long-bracket string ([==[ … ]==]) with
// a level that does not occur in s, so any body (quotes, backslashes,
// newlines) round-trips byte-for-byte. The leading newline is required:
// Lua drops the first newline after the opening bracket.
func luaLongString(s string) string {
	eq := ""
	for strings.Contains(s, "]"+eq+"]") {
		eq += "="
	}
	return "[" + eq + "[\n" + s + "]" + eq + "]"
}

// ─── Disk I/O ──────────────────────────────────────────────────────────

func userSnippetsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, userSnippetsFile), nil
}

// loadUserSnippets reads snippets.json. A missing file is empty, not an
// error; malformed JSON is returned so the UI can say so.
func loadUserSnippets() (userSnippets, error) {
	path, err := userSnippetsPath()
	if err != nil {
		return userSnippets{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return userSnippets{}, nil
		}
		return userSnippets{}, err
	}
	return parseUserSnippets(b)
}

func saveUserSnippets(u userSnippets) error {
	path, err := userSnippetsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, u.marshal(), 0o644)
}

const defaultSnippetsJSON = `{
  "*": {
    "todo": "// TODO(${1:me}): $0"
  },
  "go": {
    "errw": [
      "if err != nil {",
      "\treturn fmt.Errorf(\"${1:context}: %w\", err)",
      "}",
      "$0"
    ]
  }
}
`

// ─── nvim wiring ───────────────────────────────────────────────────────

// userSnippetsSetupLua loads snippets.json into the Lua side and reloads
// it every time the file is written from inside termocode.
func userSnippetsSetupLua(path string) string {
	return fmt.Sprintf(`
local path = %s
termocode_load_user_snippets(path)
local group = vim.api.nvim_create_augroup('TermocodeUserSnippets', { clear = true })
vim.api.nvim_create_autocmd('BufWritePost', {
  group = group,
  pattern = '*.json',
  callback = function(ev)
    if vim.fn.fnamemodify(ev.file, ':p') == path then
      termocode_load_user_snippets(path)
    end
  end,
})
`, luaLongString(path))
}

// setupUserSnippets runs once after snippetsLua at startup.
func (m *Model) setupUserSnippets() {
	if m.nvim == nil {
		return
	}
	path, err := userSnippetsPath()
	if err != nil {
		return
	}
	_ = m.nvim.ExecLua(userSnippetsSetupLua(path))
}

// reloadUserSnippets re-reads snippets.json into nvim after a change.
func (m *Model) reloadUserSnippets() {
	if m.nvim == nil {
		return
	}
	path, err := userSnippetsPath()
	if err != nil {
		return
	}
	_ = m.nvim.ExecLua("termocode_load_user_snippets(" + luaLongString(path) + ")")
}

// currentFiletype returns the active buffer's nvim filetype ("" if none).
func (m *Model) currentFiletype() string {
	if m.nvim == nil {
		return ""
	}
	ft, err := m.nvim.EvalLuaString(`return vim.bo.filetype`)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(ft)
}

// ─── Manager UI ────────────────────────────────────────────────────────

const (
	snipMgrNew    = "snipmgr-new"
	snipMgrFile   = "snipmgr-file"
	snipMgrScope  = "snipmgr-scope"
	snipMgrPrefix = "snipmgr-item:"
)

// openSnippetManager shows the user snippets with New / Open JSON /
// scope-filter rows on top. Selecting a snippet asks Edit / Delete.
func (m *Model) openSnippetManager() tea.Cmd {
	snips, err := loadUserSnippets()
	if err != nil {
		return m.toastErr("snippets.json is not valid JSON", err)
	}
	scopes := snips.scopes()
	// Drop a stale filter (its last snippet was deleted).
	if m.extras.snipScope != "" && snips[m.extras.snipScope] == nil {
		m.extras.snipScope = ""
	}
	filterLabel := "all"
	if m.extras.snipScope != "" {
		filterLabel = m.extras.snipScope
	}
	items := []picker.Item{
		{ID: snipMgrNew, Title: "+ New Snippet...", Hint: "scope:trigger"},
		{ID: snipMgrFile, Title: "Open snippets.json", Hint: "edit by hand"},
		{ID: snipMgrScope, Title: "Filter Scope: " + filterLabel, Hint: fmt.Sprintf("%d scopes · Enter to cycle", len(scopes))},
	}
	for _, s := range snips.list(m.extras.snipScope) {
		preview := firstLine(s.Body)
		if len(preview) > 40 {
			preview = preview[:40] + "…"
		}
		items = append(items, picker.Item{
			ID:    snipMgrPrefix + s.Scope + "\x00" + s.Trigger,
			Title: s.Trigger,
			Hint:  "[" + s.Scope + "] " + preview,
		})
	}
	m.picker = picker.NewItems(" Snippets: Manage ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindSnippetManager
	return m.picker.Init()
}

func (m *Model) onSnippetManagerSelected(id string) tea.Cmd {
	switch id {
	case snipMgrNew:
		def := m.currentFiletype()
		if def == "" {
			def = globalSnippetScope
		}
		if m.extras.snipScope != "" {
			def = m.extras.snipScope
		}
		m.prompt = prompt.New("New Snippet", "scope:trigger (scope = filetype or *):", def+":")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindSnippetNew
		return nil
	case snipMgrFile:
		return m.editConfigFile(userSnippetsFile, defaultSnippetsJSON)
	case snipMgrScope:
		snips, _ := loadUserSnippets()
		m.extras.snipScope = nextSnippetScope(snips.scopes(), m.extras.snipScope)
		return m.openSnippetManager()
	}
	if !strings.HasPrefix(id, snipMgrPrefix) {
		return nil
	}
	scope, trigger, ok := strings.Cut(strings.TrimPrefix(id, snipMgrPrefix), "\x00")
	if !ok {
		return nil
	}
	snips, _ := loadUserSnippets()
	body := snips[scope][trigger]
	m.extras.snipTarget = snippetRef{Scope: scope, Trigger: trigger}
	m.confirm = confirm.New(
		fmt.Sprintf("Snippet %q [%s]", trigger, scope),
		body,
		[]confirm.Button{
			{ID: "edit", Title: "Edit", Style: confirm.StylePrimary},
			{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindSnippetAction
	return nil
}

// onSnippetActionConfirm handles Edit / Delete for one snippet.
func (m *Model) onSnippetActionConfirm(id string) tea.Cmd {
	ref := m.extras.snipTarget
	m.extras.snipTarget = snippetRef{}
	switch id {
	case "edit":
		cmd := m.editConfigFile(userSnippetsFile, defaultSnippetsJSON)
		m.jumpToSnippetInBuffer(ref)
		return cmd
	case "delete":
		snips, err := loadUserSnippets()
		if err != nil {
			return m.toastErr("snippets.json is not valid JSON", err)
		}
		if !snips.remove(ref) {
			return nil
		}
		if err := saveUserSnippets(snips); err != nil {
			return m.toastErr("Snippet error", err)
		}
		m.reloadUserSnippets()
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "Snippet deleted", "["+ref.Scope+"] "+ref.Trigger)
		return c
	}
	return nil
}

// jumpToSnippetInBuffer moves the cursor in the freshly opened
// snippets.json to the trigger's key inside its scope block.
func (m *Model) jumpToSnippetInBuffer(ref snippetRef) {
	if m.nvim == nil || ref.Trigger == "" {
		return
	}
	_ = m.nvim.ExecLua(fmt.Sprintf(`
local scope, trig = %s, %s
local lines = vim.api.nvim_buf_get_lines(0, 0, -1, false)
local in_scope = false
for i, l in ipairs(lines) do
  local key = l:match('^%%s*"(.-)"%%s*:')
  if key and l:match('^  "') then in_scope = (key == scope) end
  if in_scope and key == trig and not l:match('^  "') then
    vim.api.nvim_win_set_cursor(0, { i, (l:find('"') or 1) - 1 })
    return
  end
end
`, luaLongString(ref.Scope), luaLongString(ref.Trigger)))
}

// onSnippetNewSubmit is step 1 of New Snippet: "scope:trigger".
func (m *Model) onSnippetNewSubmit(value string) tea.Cmd {
	scope, trigger, err := parseScopeTrigger(value)
	if err != nil {
		return m.toastErr("Bad snippet name", err)
	}
	m.extras.snipNewScope, m.extras.snipNewTrigger = scope, trigger
	initial := ""
	if snips, err := loadUserSnippets(); err == nil {
		// Editing an existing trigger: seed the prompt with its body.
		initial = strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\t", "\\t").Replace(snips[scope][trigger])
	}
	m.prompt = prompt.New("New Snippet ["+scope+"] "+trigger,
		`Body (\n = new line, \t = tab, ${1:name} = tab stop, $0 = end):`, initial)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindSnippetBody
	return nil
}

// onSnippetBodySubmit is step 2 of New Snippet: save the body.
func (m *Model) onSnippetBodySubmit(value string) tea.Cmd {
	scope, trigger := m.extras.snipNewScope, m.extras.snipNewTrigger
	m.extras.snipNewScope, m.extras.snipNewTrigger = "", ""
	if trigger == "" || strings.TrimSpace(value) == "" {
		return nil
	}
	snips, err := loadUserSnippets()
	if err != nil {
		return m.toastErr("snippets.json is not valid JSON", err)
	}
	replaced := snips.set(scope, trigger, unescapeBodyInput(value))
	if err := saveUserSnippets(snips); err != nil {
		return m.toastErr("Snippet error", err)
	}
	m.reloadUserSnippets()
	title := "Snippet added"
	if replaced {
		title = "Snippet updated"
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, title, "["+scope+"] "+trigger)
	return c
}
