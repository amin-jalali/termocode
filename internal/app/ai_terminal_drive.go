package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CMP-67 Full Terminal Use: the agent drives a program that runs inside
// the visible terminal (psql, gdb, python, a REPL), and the user can take
// over at any moment.
//
//   - terminal_read knows REPL prompts: a shell that runs psql is "busy"
//     forever, so the read stops once the program shows its prompt again
//     and quiet output follows. until (a regex) stops on any text.
//   - terminal_keys presses keys: ctrl-c, ctrl-d, arrows, tab, esc …
//   - A password or passphrase prompt stops the read, and terminal_send
//     refuses to type there: the user types secrets, the AI never does.
//   - Take over: the first time the agent types in a tab, a toast says so.
//     When the user types in that tab, it is theirs: the agent's next
//     terminal_send / terminal_keys there fails and tells the model to ask
//     in chat. "Terminal: Take Over from the AI" does the same without
//     typing; "Terminal: Hand Back to the AI" gives the tabs back. Only
//     the user runs those two commands (AI policy: never).

// aiTermDriveState is one tab the agent typed in.
type aiTermDriveState struct {
	takenOver bool
}

const aiTermHandBackTitle = "Terminal: Hand Back to the AI"

// aiTermLabelOf is "tab 2 (name)" for buffer buf, or "" when no tab has it.
func (m *Model) aiTermLabelOf(buf int) string {
	for _, tabs := range [][]terminalTab{m.terminalTabs, m.hiddenTerminalTabs} {
		for i, t := range tabs {
			if t.BufID == buf {
				return aiTermLabel(i, t.Name)
			}
		}
	}
	return ""
}

// aiTermDrivePrune forgets tabs that were closed.
func (m *Model) aiTermDrivePrune() {
	for buf := range m.aiTermDrive {
		if m.aiTermLabelOf(buf) == "" {
			delete(m.aiTermDrive, buf)
		}
	}
}

// aiTermDriveNote runs on Update after the agent typed in buf. The first
// time it tells the user how to take over.
func (m *Model) aiTermDriveNote(buf int) tea.Cmd {
	m.aiTermDrivePrune()
	if _, ok := m.aiTermDrive[buf]; ok {
		return nil
	}
	if m.aiTermDrive == nil {
		m.aiTermDrive = map[int]*aiTermDriveState{}
	}
	m.aiTermDrive[buf] = &aiTermDriveState{}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "AI is typing in terminal "+m.aiTermLabelOf(buf),
		"Type in that tab to take over; the AI then stops typing there.")
	return c
}

// aiTermDriving reports whether the agent types in buf and the user has
// not taken it over.
func (m *Model) aiTermDriving(buf int) bool {
	st, ok := m.aiTermDrive[buf]
	return ok && !st.takenOver
}

// aiTermTakenOver reports whether the user took buf over from the agent.
func (m *Model) aiTermTakenOver(buf int) bool {
	st, ok := m.aiTermDrive[buf]
	return ok && st.takenOver
}

// aiTermDriveCheck runs on Update before the agent types in buf.
func (m *Model) aiTermDriveCheck(buf int) error {
	if !m.aiTermTakenOver(buf) {
		return nil
	}
	return fmt.Errorf("the user took over terminal %s (they typed in it): do not type there. "+
		"Ask in chat what to do; the user gives it back with the palette command %q",
		m.aiTermLabelOf(buf), aiTermHandBackTitle)
}

// aiTermNoteUserKey runs on Update when a user key goes into the
// terminal: typing in a tab the agent drives takes it over.
func (m *Model) aiTermNoteUserKey() tea.Cmd {
	if len(m.aiTermDrive) == 0 {
		return nil
	}
	tab := m.activeTerminalTab()
	if tab == nil {
		return nil
	}
	buf := tab.focusedBuf()
	st, ok := m.aiTermDrive[buf]
	if !ok || st.takenOver {
		return nil
	}
	st.takenOver = true
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "You took over terminal "+m.aiTermLabelOf(buf),
		"The AI stops typing there. Give it back with "+aiTermHandBackTitle+".")
	return c
}

// runTermDriveAction runs the take-over / hand-back palette commands.
func (m *Model) runTermDriveAction(id string) (tea.Cmd, bool) {
	var takeOver bool
	switch id {
	case "terminal-ai-take-over":
		takeOver = true
	case "terminal-ai-hand-back":
	default:
		return nil, false
	}
	m.aiTermDrivePrune()
	n := 0
	for _, st := range m.aiTermDrive {
		if st.takenOver != takeOver {
			st.takenOver = takeOver
			n++
		}
	}
	var title string
	switch {
	case n == 0 && takeOver:
		title = "The AI is not typing in any terminal"
	case n == 0:
		title = "No terminal to hand back to the AI"
	case takeOver:
		title = fmt.Sprintf("You took over %d terminal tab(s); the AI stops typing there", n)
	default:
		title = fmt.Sprintf("Handed %d terminal tab(s) back to the AI", n)
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, title)
	return c, true
}

// REPL prompts, matched on the last line without trailing spaces.
var aiTermPrompts = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"gdb", regexp.MustCompile(`^\(gdb\)$`)},
	{"lldb", regexp.MustCompile(`^\(lldb\)$`)},
	{"pdb", regexp.MustCompile(`^\(Pdb\)$`)},
	{"python", regexp.MustCompile(`^(>>>|\.\.\.)$`)},
	{"mysql", regexp.MustCompile(`^(mysql|MariaDB \[[^\]]*\])>$`)},
	{"sqlite", regexp.MustCompile(`^sqlite>$`)},
	{"irb", regexp.MustCompile(`^irb\([^)]*\):\d+:\d+[>*"']$`)},
	{"iex", regexp.MustCompile(`^iex\(\d+\)>$`)},
	{"ghci", regexp.MustCompile(`^(ghci|Prelude[^>]*)>$`)},
	{"redis", regexp.MustCompile(`^(redis )?[\w.\-]+:\d+(\[\d+\])?>$`)},
	{"psql", regexp.MustCompile(`^[\w.\-]*\w[=\-(][#>]$`)},
	{"repl", regexp.MustCompile(`^>$`)},
}

// aiTermPromptKind names the REPL whose prompt line is, or "".
func aiTermPromptKind(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	for _, p := range aiTermPrompts {
		if p.re.MatchString(line) {
			return p.kind
		}
	}
	return ""
}

var aiTermSecretRe = regexp.MustCompile(`(?i)\b(password|passphrase|passcode|pin)\b.*:\s*$`)

// aiTermAsksSecret reports whether line is a password-like prompt.
func aiTermAsksSecret(line string) bool {
	return aiTermSecretRe.MatchString(line)
}

// aiTermLastLine is the last line of a tail.
func aiTermLastLine(tail string) string {
	if i := strings.LastIndexByte(tail, '\n'); i >= 0 {
		return tail[i+1:]
	}
	return tail
}

// aiTermReadDone decides when terminal_read(wait_ms) may stop. tail is
// the last few lines; why says what ended the wait.
func aiTermReadDone(started bool, busy int, quiet time.Duration, tail string, until *regexp.Regexp) (bool, string) {
	if !started {
		return false, ""
	}
	last := aiTermLastLine(tail)
	if aiTermAsksSecret(last) && quiet >= aiTermQuietIdle {
		return true, "password"
	}
	if until != nil {
		if until.MatchString(tail) {
			return true, "until"
		}
		return false, ""
	}
	switch {
	case busy == 0 && quiet >= aiTermQuietIdle:
		return true, "idle"
	case busy < 0 && quiet >= aiTermQuietUnknown:
		return true, "quiet"
	case busy == 1 && quiet >= aiTermQuietIdle && aiTermPromptKind(last) != "":
		return true, "prompt"
	}
	return false, ""
}

// aiTermKeyNames are the keys terminal_keys knows, besides ctrl-a … ctrl-z.
var aiTermKeyNames = map[string]string{
	"enter": "\r", "return": "\r", "tab": "\t", "esc": "\x1b", "escape": "\x1b",
	"backspace": "\x7f", "space": " ", "up": "\x1b[A", "down": "\x1b[B",
	"right": "\x1b[C", "left": "\x1b[D", "home": "\x1b[H", "end": "\x1b[F",
	"pageup": "\x1b[5~", "pagedown": "\x1b[6~", "delete": "\x1b[3~", "del": "\x1b[3~",
	"ctrl-\\": "\x1c", "ctrl-]": "\x1d", "ctrl-[": "\x1b",
}

const aiTermKeyHelp = "ctrl-c, ctrl-d, ctrl-a … ctrl-z, enter, tab, esc, up, down, left, right, home, end, pageup, pagedown, backspace, delete, space"

// aiTermKeyBytes turns "ctrl-c" or "up enter" into the bytes to send.
func aiTermKeyBytes(spec string) (string, error) {
	fields := strings.FieldsFunc(spec, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	if len(fields) == 0 {
		return "", errors.New("keys is empty; use " + aiTermKeyHelp)
	}
	var b strings.Builder
	for _, f := range fields {
		k := strings.ReplaceAll(strings.ToLower(f), "+", "-")
		if s, ok := aiTermKeyNames[k]; ok {
			b.WriteString(s)
			continue
		}
		if c, ok := strings.CutPrefix(k, "ctrl-"); ok && len(c) == 1 && c[0] >= 'a' && c[0] <= 'z' {
			b.WriteByte(c[0] - 'a' + 1)
			continue
		}
		return "", fmt.Errorf("unknown key %q; use %s (type text with terminal_send)", f, aiTermKeyHelp)
	}
	return b.String(), nil
}

// aiTermLastLineOf runs on Update: the last non-blank line of buf.
func (m *Model) aiTermLastLineOf(buf int) string {
	var raw string
	if m.nvim == nil || m.nvim.ExecLuaResult(aiTermSnapLua, &raw, buf, 5) != nil || raw == "" {
		return ""
	}
	var s aiTermSnap
	if json.Unmarshal([]byte(raw), &s) != nil {
		return ""
	}
	return aiTermLastLine(aiTermTail(s.Lines, 1))
}

// aiTermKeys is terminal_keys on the tool goroutine: it never opens a
// new shell (keys make no sense in a fresh prompt).
func aiTermKeys(ctx context.Context, call aiIDECaller, turn *aiTermTurn, tabArg, keys string) (string, error) {
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if m.nvim == nil {
			return "", nil, errors.New("the editor is not running")
		}
		if tabs, _, _ := m.aiTermTabs(); len(tabs) == 0 {
			return "", nil, errAINoTerminal
		}
		buf, _, err := m.aiTermPick(tabArg, false)
		if err != nil {
			return "", nil, err
		}
		return m.aiTermTypeAs(buf, keys, "", true, turn)
	})
}

func aiTermKeysTool(call aiIDECaller, turn *aiTermTurn, tabParam map[string]any) ai.Tool {
	return ai.Tool{
		Name: "terminal_keys",
		Description: "Press keys in an integrated terminal tab, for a program that runs there (psql, gdb, a REPL, a pager). " +
			"ctrl-c stops a command, ctrl-d ends input or quits a REPL, up / down walk the history, tab completes; " +
			"several keys are space-separated, like \"up enter\". Type text with terminal_send; read the result with terminal_read.",
		Params: map[string]any{
			"keys": map[string]any{"type": "string", "description": "keys to press: " + aiTermKeyHelp},
			"tab":  tabParam,
		},
		Required: []string{"keys"},
		Mutating: true, Class: ai.ClassRun,
		Describe: func(a map[string]any) string {
			s := "Press in terminal: " + ai.ArgString(a, "keys")
			if t := ai.ArgString(a, "tab"); t != "" {
				s += " (tab " + t + ")"
			}
			return s
		},
		Validate: func(a map[string]any) error {
			_, err := aiTermKeyBytes(ai.ArgString(a, "keys"))
			return err
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			keys, err := aiTermKeyBytes(ai.ArgString(a, "keys"))
			if err != nil {
				return "", err
			}
			return aiTermKeys(ctx, call, turn, ai.ArgString(a, "tab"), keys)
		},
	}
}

// aiTermStateNote adds what the program in the tab waits for to the
// terminal_read head.
func aiTermStateNote(busy int, tail string, takenOver bool) string {
	var s string
	last := aiTermLastLine(tail)
	switch {
	case aiTermAsksSecret(last):
		s = ", it asks for a password: the user types it there (never type or ask for secrets)"
	case busy != 0:
		if k := aiTermPromptKind(last); k != "" {
			s = ", " + k + " waits for input (terminal_send types, terminal_keys presses ctrl-c / ctrl-d …)"
		}
	}
	if takenOver {
		s += ", the user took over this tab (do not type there; ask in chat)"
	}
	return s
}

const aiNoTermDrive = "only the user takes a terminal over from the AI or hands it back; if a tab was taken over, ask in chat"
