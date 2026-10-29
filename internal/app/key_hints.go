package app

import (
	"encoding/json"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/keyhint"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Key hints: after the leader key in the editor, a small popup lists the
// next keys (internal/keyhint). The key_hints setting turns it off.
//
// Scope: termocode's own keymap has no multi-key chords, so the popup
// covers the editor's leader mappings (the user's and termocode's own,
// e.g. <leader>f). The mappings come from nvim, so the popup always shows
// what is bound right now. The popup only watches: every key still goes
// to the editor, except Backspace that goes up a level.

// keyHintsEnabled is the key_hints setting (default on).
func keyHintsEnabled(cfg settingsConfig) bool { return cfg.KeyHints == nil || *cfg.KeyHints }

// setKeyHints remembers the setting; off also closes the popup.
func (m *Model) setKeyHints(on bool) {
	m.hintsOff, m.hintsInit = !on, true
	if !on && m.hints != nil {
		m.hints.Reset()
	}
}

// toggleKeyHints flips key_hints and says what it is now.
func (m *Model) toggleKeyHints() tea.Cmd {
	cfg := loadSettings()
	on := !keyHintsEnabled(cfg)
	cfg.KeyHints = &on
	var c tea.Cmd
	if err := saveSettings(cfg); err != nil {
		m.toast, c = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
		return c
	}
	m.setKeyHints(on)
	if on {
		m.toast, c = m.toast.Push(toast.Info, "Key hints on")
	} else {
		m.toast, c = m.toast.Push(toast.Info, "Key hints off")
	}
	return c
}

// hintSource is where the popup learns the leader and the mappings.
type hintSource interface {
	// Leader is the leader key as a token ("<space>" or "\").
	Leader() string
	// Timeout is how long the editor waits for the next key (0 = forever).
	Timeout() time.Duration
	// Maps lists the normal-mode mappings of the current buffer.
	Maps() []keyhint.Mapping
	// Send types keys into the editor.
	Send(keys string)
}

// nvimHintSource reads them from nvim.
type nvimHintSource struct{ c *nvim.Client }

type nvimHintInfo struct {
	Leader  string `json:"leader"`
	Timeout int    `json:"timeout"`
	Maps    []struct {
		LHS  string `json:"lhs"`
		Desc string `json:"desc"`
		RHS  string `json:"rhs"`
	} `json:"maps"`
}

const nvimHintLua = `
local maps = {}
for _, list in ipairs({ vim.api.nvim_get_keymap('n'), vim.api.nvim_buf_get_keymap(0, 'n') }) do
  for _, k in ipairs(list) do
    table.insert(maps, { lhs = k.lhs, desc = k.desc or '', rhs = k.rhs or '' })
  end
end
return vim.json.encode({
  leader = vim.g.mapleader or '\\',
  timeout = vim.o.timeout and vim.o.timeoutlen or 0,
  maps = maps,
})`

func (s nvimHintSource) info() nvimHintInfo {
	var in nvimHintInfo
	if s.c == nil {
		return in
	}
	out, err := s.c.EvalLuaString(nvimHintLua)
	if err != nil || json.Unmarshal([]byte(out), &in) != nil {
		return nvimHintInfo{}
	}
	return in
}

func (s nvimHintSource) Leader() string {
	l := s.info().Leader
	if l == "" {
		return "\\"
	}
	if t := keyhint.Tokens(l); len(t) > 0 {
		if t[0] == " " {
			return "<space>"
		}
		return t[0]
	}
	return "\\"
}

func (s nvimHintSource) Timeout() time.Duration {
	return time.Duration(s.info().Timeout) * time.Millisecond
}

func (s nvimHintSource) Maps() []keyhint.Mapping {
	var out []keyhint.Mapping
	for _, k := range s.info().Maps {
		out = append(out, keyhint.Mapping{LHS: k.LHS, Desc: k.Desc, RHS: k.RHS})
	}
	return out
}

func (s nvimHintSource) Send(keys string) {
	if s.c != nil {
		_ = s.c.Input(keys)
	}
}

// keyHintTickMsg fires after a wait the tracker asked for.
type keyHintTickMsg struct{ gen int }

func keyHintTick(gen int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return keyHintTickMsg{gen} })
}

// onKeyHintTick runs the tracker's delayed check.
func (m *Model) onKeyHintTick(msg keyHintTickMsg) tea.Cmd {
	if m.hints == nil {
		return nil
	}
	if w := m.hints.Tick(msg.gen); w > 0 {
		return keyHintTick(msg.gen, w)
	}
	return nil
}

// hintToken is the key as the tracker names it; "" for a key that is not
// one editor key (arrows, function keys, mouse).
func hintToken(k tea.KeyMsg) string {
	switch k.Type {
	case tea.KeyRunes:
		if len(k.Runes) == 1 && !k.Alt {
			return string(k.Runes)
		}
	case tea.KeySpace:
		return "<space>"
	case tea.KeyEsc:
		return "<esc>"
	case tea.KeyBackspace:
		return "<bs>"
	case tea.KeyEnter:
		return "<cr>"
	case tea.KeyTab:
		return "<tab>"
	}
	return ""
}

// hintKeys turns tokens back into text nvim_input understands.
func hintKeys(toks []string) string {
	var b strings.Builder
	for _, t := range toks {
		if t == "<" {
			t = "<lt>"
		}
		b.WriteString(t)
	}
	return b.String()
}

// hintsReady reports whether a key now belongs to the editor in normal
// mode, where a leader sequence can start.
func (m *Model) hintsReady() bool {
	if !m.hintsInit {
		m.hintsOff, m.hintsInit = !keyHintsEnabled(loadSettings()), true
	}
	if m.hintsOff || m.focus != FocusEditor || m.inTerminal {
		return false
	}
	if m.editorMode != "" && m.editorMode != "normal" {
		return false
	}
	return m.hintSrc != nil || (m.nvim != nil && m.nvimAttached)
}

// keyHintKey lets the tracker see a key before the normal key handling.
// swallow is true only for Backspace that went up a level (the prefix is
// then sent to the editor again).
func (m *Model) keyHintKey(k tea.KeyMsg) (swallow bool, cmd tea.Cmd) {
	active := m.hints != nil && m.hints.Active()
	if !active && !m.hintsReady() {
		return false, nil
	}
	if active && (!m.hintsReady() || m.keys.Match(k) != keymap.ActionNone) {
		m.hints.Reset() // the key goes elsewhere: the sequence is over
		return false, nil
	}
	tok := hintToken(k)
	if tok == "" {
		if active {
			m.hints.Reset()
		}
		return false, nil
	}
	if m.hints == nil {
		src := m.hintSrc
		if src == nil {
			src = nvimHintSource{m.nvim}
		}
		m.hints = &keyhint.Tracker{Leader: src.Leader(), Expire: src.Timeout(), Load: src.Maps}
		m.hintSrc = src
	}
	out := m.hints.Press(tok)
	if out.Up {
		m.hintSrc.Send("<Esc>" + hintKeys(out.Prefix))
		swallow = true
	}
	if out.Wait > 0 {
		cmd = keyHintTick(out.Gen, out.Wait)
	}
	return swallow, cmd
}

// handleKeyWithHints is the key entry point: the tracker looks first.
func (m Model) handleKeyWithHints(msg tea.KeyMsg) (Model, tea.Cmd) {
	swallow, tick := m.keyHintKey(msg)
	if swallow {
		return m, tick
	}
	m, cmd := m.handleGlobalKey(msg)
	return m, tea.Batch(tick, cmd)
}

// overlayKeyHints draws the popup over the frame.
func (m Model) overlayKeyHints(base string) string {
	if m.hints == nil {
		return base
	}
	c, ok := m.hints.Content()
	if !ok {
		return base
	}
	rows, x, y, ok := keyhint.Layout(c, m.w, m.h)
	if !ok {
		return base
	}
	lines := strings.Split(base, "\n")
	for i, r := range rows {
		if y+i < 0 || y+i >= len(lines) {
			continue
		}
		lines[y+i] = spliceAt(lines[y+i], r, x)
	}
	return strings.Join(lines, "\n")
}
