package addai

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clilogin"
	"github.com/amin-jalali/termocode/internal/connectcard"
)

// testKey is a fake key, split so no scanner takes it for a real one.
var testKey = "sk-" + "test-" + "0000aaaa1111"

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func runes(s string) tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func newCard(conn map[string]ai.ProviderEntry) Model {
	m := New(Options{Connected: conn, KeyNote: "Accepts: a value or a reference."})
	m.SetSize(120, 40)
	return m
}

// do runs one input and returns the message the card sent out (nil: none).
func do(t *testing.T, m *Model, msg tea.Msg) tea.Msg {
	t.Helper()
	var cmd tea.Cmd
	*m, cmd = m.Update(msg)
	if cmd == nil {
		return nil
	}
	return cmd()
}

func press(t *testing.T, m *Model, id string) tea.Msg {
	t.Helper()
	return do(t, m, connectcard.ButtonMsg{ID: id})
}

// pick chooses kind on the first card with the keys (↓ to it, Enter).
func pick(t *testing.T, m *Model, kind string) tea.Msg {
	t.Helper()
	for i, e := range Catalog() {
		if e.Kind == kind {
			for j := 0; j < i; j++ {
				do(t, m, key(tea.KeyDown))
			}
			return do(t, m, key(tea.KeyEnter))
		}
	}
	t.Fatalf("no %s in the list", kind)
	return nil
}

func text(m Model) string { return ansi.Strip(m.Box()) }

func buttonIDs(m Model) []string {
	var out []string
	for _, b := range m.Card().Buttons {
		out = append(out, b.ID)
	}
	return out
}

// Spec: the first card lists the AIs in the approved order, says which
// are added already, and never offers the old OAuth sign-in kind (Q3) or
// the word "slot".
func TestPickList(t *testing.T) {
	m := newCard(map[string]ai.ProviderEntry{ai.KindOllama: {Kind: ai.KindOllama, Method: ai.MethodLocal}})
	var names []string
	for _, e := range Catalog() {
		names = append(names, e.Name)
	}
	want := []string{"Anthropic", "OpenAI", "Ollama", "OpenRouter", "LM Studio", "Other server"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("catalog %v, want %v", names, want)
	}
	s := text(m)
	for _, w := range append(want, "Add an AI", "Claude models", "added") {
		if !strings.Contains(s, w) {
			t.Errorf("card misses %q:\n%s", w, s)
		}
	}
	for _, bad := range []string{"OAuth", "slot", "Slot"} {
		if strings.Contains(s, bad) {
			t.Errorf("card says %q:\n%s", bad, s)
		}
	}
	if got := buttonIDs(m); !reflect.DeepEqual(got, []string{"cancel"}) {
		t.Errorf("buttons %v", got)
	}
}

// at builds a card at one step (no provider connected).
func at(t *testing.T, step string) Model {
	t.Helper()
	m := newCard(nil)
	switch step {
	case "pick":
	case "method":
		pick(t, &m, ai.KindAnthropic)
	case "key":
		pick(t, &m, ai.KindOpenRouter)
	case "key-busy":
		pick(t, &m, ai.KindOpenRouter)
		do(t, &m, runes(testKey))
		do(t, &m, key(tea.KeyEnter))
	case "url":
		pick(t, &m, ai.KindCompatible)
	case "local":
		pick(t, &m, ai.KindOllama)
	case "local-bad":
		pick(t, &m, ai.KindOllama)
		m.Fail(m.Gen(), "Ollama is not running on this machine.")
	case "signin-bad":
		pick(t, &m, ai.KindAnthropic)
		do(t, &m, key(tea.KeyEnter))
		press(t, &m, "signin")
		m.Fail(m.Gen(), "Could not start the Claude Code sign-in. Try again, or use an API key.")
	case "signin-ask":
		pick(t, &m, ai.KindOpenAI)
		do(t, &m, key(tea.KeyEnter))
	case "signin-install", "signin-browser", "signin-code", "signin-device", "signin-models", "signin-nonode":
		pick(t, &m, ai.KindOpenAI)
		do(t, &m, key(tea.KeyEnter))
		press(t, &m, "signin")
		m.Progress(m.Gen(), clilogin.State{Phase: clilogin.PhaseInstalling, Elapsed: 72 * time.Second, Line: "added 12 packages"})
		switch step {
		case "signin-browser":
			m.Progress(m.Gen(), clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: testLink})
		case "signin-code":
			m.Progress(m.Gen(), clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: testLink, NeedsCode: true})
		case "signin-device":
			m.Progress(m.Gen(), clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: testLink, Code: "ABCD-1234"})
		case "signin-models":
			m.Progress(m.Gen(), clilogin.State{Phase: clilogin.PhaseLoadingModels})
		case "signin-nonode":
			m.Progress(m.Gen(), clilogin.State{Phase: clilogin.PhaseFailed, Err: &clilogin.Error{Kind: clilogin.FailNoNode, Msg: "Codex needs Node.js."}})
		}
	case "done":
		pick(t, &m, ai.KindOllama)
		m.Finish(m.Gen(), []string{"llama3.2:3b"})
	default:
		t.Fatalf("no step %q", step)
	}
	return m
}

// Spec: every button and Esc of every step goes where the flow says:
// Back one step, Cancel / Esc close without a result, the actions send
// one request to the app.
func TestStepTable(t *testing.T) {
	type res struct {
		step Step
		out  string // the type of the message sent ("" none)
	}
	cases := []struct {
		at, input string
		want      res
	}{
		{"pick", "esc", res{StepPick, "CancelMsg"}},
		{"pick", "cancel", res{StepPick, "CancelMsg"}},
		{"method", "back", res{StepPick, ""}},
		{"method", "cancel", res{StepMethod, "CancelMsg"}},
		{"method", "esc", res{StepMethod, "CancelMsg"}},
		{"method", "enter", res{StepSignIn, ""}}, // the ask card: nothing runs before Install and sign in
		{"key", "back", res{StepPick, ""}},
		{"key", "cancel", res{StepKey, "CancelMsg"}},
		{"key", "esc", res{StepKey, "CancelMsg"}},
		{"key", "connect", res{StepKey, ""}}, // empty: an inline error, no request
		{"key-busy", "cancel", res{StepKey, "CancelMsg"}},
		{"key-busy", "esc", res{StepKey, "CancelMsg"}},
		{"url", "back", res{StepPick, ""}},
		{"url", "cancel", res{StepURL, "CancelMsg"}},
		{"url", "connect", res{StepURL, "ConnectURLMsg"}}, // the default URL, no key
		{"url", "esc", res{StepURL, "CancelMsg"}},
		{"local", "cancel", res{StepLocal, "CancelMsg"}},
		{"local", "esc", res{StepLocal, "CancelMsg"}},
		{"local-bad", "retry", res{StepLocal, "DetectLocalMsg"}},
		{"local-bad", "back", res{StepPick, ""}},
		{"local-bad", "cancel", res{StepLocal, "CancelMsg"}},
		{"signin-bad", "retry", res{StepSignIn, "SignInMsg"}},
		{"signin-bad", "apikey", res{StepKey, ""}},
		{"signin-bad", "cancel", res{StepSignIn, "CancelMsg"}},
		{"signin-bad", "esc", res{StepSignIn, "CancelMsg"}},
		{"done", "done", res{StepDone, "DoneMsg"}},
		{"done", "esc", res{StepDone, "DoneMsg"}},
	}
	for _, c := range cases {
		t.Run(c.at+"/"+c.input, func(t *testing.T) {
			m := at(t, c.at)
			var out tea.Msg
			switch c.input {
			case "esc":
				out = do(t, &m, key(tea.KeyEsc))
			case "enter":
				out = do(t, &m, key(tea.KeyEnter))
			default:
				found := false
				for _, id := range buttonIDs(m) {
					found = found || id == c.input
				}
				if !found {
					t.Fatalf("no %q button on %s (%v)", c.input, c.at, buttonIDs(m))
				}
				out = press(t, &m, c.input)
			}
			got := res{m.Step(), ""}
			if out != nil {
				got.out = strings.TrimPrefix(reflect.TypeOf(out).String(), "addai.")
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// Spec: Connect sends the typed key to the app once, the card shows the
// busy line, the key never shows on the card, and a failed check keeps
// the key in the field with one plain sentence.
func TestKeyConnect(t *testing.T) {
	m := newCard(nil)
	if out := pick(t, &m, ai.KindAnthropic); out != nil {
		t.Fatalf("method choice sent %T", out)
	}
	do(t, &m, key(tea.KeyDown)) // Use an API key
	do(t, &m, key(tea.KeyEnter))
	if m.Step() != StepKey {
		t.Fatalf("step %v", m.Step())
	}
	do(t, &m, runes(testKey))
	if strings.Contains(text(m), testKey) || strings.Contains(text(m), "test-0000") {
		t.Fatal("the key shows on the card")
	}
	out := do(t, &m, key(tea.KeyEnter))
	msg, ok := out.(ConnectKeyMsg)
	if !ok || msg.Kind != ai.KindAnthropic || msg.Key != testKey || msg.Gen != m.Gen() {
		t.Fatalf("Enter sent %#v", out)
	}
	if !m.Busy() || !strings.Contains(text(m), "Checking the key and loading models") {
		t.Errorf("no busy line:\n%s", text(m))
	}
	if again := press(t, &m, "connect"); again != nil {
		t.Errorf("a second Connect while busy sent %T", again)
	}
	if !m.Fail(m.Gen(), "That key was not accepted.") {
		t.Fatal("Fail ignored")
	}
	if m.Busy() || m.Step() != StepKey || m.Value("key") != testKey {
		t.Errorf("after the failure: busy %v step %v key kept %v", m.Busy(), m.Step(), m.Value("key") == testKey)
	}
	if s := text(m); !strings.Contains(s, "That key was not accepted.") || strings.Contains(s, testKey) {
		t.Errorf("card:\n%s", s)
	}
}

// Spec: a result for an older request (a Back, an Esc, a second try) is
// dropped; it never moves the card.
func TestStaleResult(t *testing.T) {
	m := at(t, "key-busy")
	old := m.Gen()
	press(t, &m, "cancel")
	if m.Finish(old, []string{"x"}) || m.Fail(old, "no") {
		t.Error("a result after Cancel was taken")
	}
	m = at(t, "local-bad")
	old = m.Gen()
	press(t, &m, "retry")
	if m.Finish(old, []string{"x"}) || m.Step() != StepLocal {
		t.Error("the old try's result was taken")
	}
	if !m.Finish(m.Gen(), []string{"llama3.2:3b", "qwen3:14b"}) || m.Step() != StepDone {
		t.Error("the new try's result was dropped")
	}
	if s := text(m); !strings.Contains(s, "Ollama is ready") || !strings.Contains(s, "qwen3:14b") || !strings.Contains(s, "2 models") {
		t.Errorf("done card:\n%s", s)
	}
}

// Spec (Q1): an AI connects one way. Picking the other way asks first;
// the card puts Keep first and lit, so Enter keeps; Replace goes on.
// The same way again asks nothing.
func TestReplaceAsksKeepFirst(t *testing.T) {
	conn := map[string]ai.ProviderEntry{ai.KindAnthropic: {Kind: ai.KindAnthropic, Method: ai.MethodKey}}
	m := newCard(conn)
	pick(t, &m, ai.KindAnthropic)
	if out := do(t, &m, key(tea.KeyEnter)); out != nil || m.Step() != StepConfirm {
		t.Fatalf("sign-in over a key: step %v sent %T", m.Step(), out)
	}
	if ids := buttonIDs(m); len(ids) != 2 || ids[0] != "keep" || ids[1] != "replace" {
		t.Fatalf("buttons %v, want keep first", ids)
	}
	if s := text(m); !strings.Contains(s, "API key") || !strings.Contains(s, "Replace") {
		t.Errorf("card:\n%s", s)
	}
	if out := do(t, &m, key(tea.KeyEnter)); out != nil || m.Step() != StepMethod {
		t.Fatalf("Enter on the question: step %v sent %T, want Keep", m.Step(), out)
	}
	do(t, &m, key(tea.KeyEnter))
	if out := press(t, &m, "replace"); m.Step() != StepSignIn || out != nil {
		t.Fatalf("Replace: step %v sent %T, want the ask card", m.Step(), out)
	}
	// Esc on the question cancels everything.
	m = newCard(conn)
	pick(t, &m, ai.KindAnthropic)
	do(t, &m, key(tea.KeyEnter))
	if _, ok := do(t, &m, key(tea.KeyEsc)).(CancelMsg); !ok {
		t.Error("Esc on the question does not cancel")
	}
	// The same way: no question.
	m = newCard(conn)
	pick(t, &m, ai.KindAnthropic)
	do(t, &m, key(tea.KeyDown))
	do(t, &m, key(tea.KeyEnter))
	if m.Step() != StepKey {
		t.Errorf("a new key for a key provider: step %v", m.Step())
	}
}

// Spec: the server URL follows the base URL rules (http(s), a host, no
// user or password); a bad one is an inline sentence that never repeats
// the URL, and nothing is sent.
func TestURLRules(t *testing.T) {
	for _, c := range []struct{ url, want string }{
		{"", "Type the server URL."},
		{"ftp://host/v1", "http:// or https://"},
		{"http://bob:" + "hunter2" + "@host:8000/v1", "user or password"},
	} {
		m := at(t, "url")
		m.SetValue("url", c.url)
		if out := press(t, &m, "connect"); out != nil {
			t.Errorf("%q sent %T", c.url, out)
		}
		failText := ""
		for _, r := range m.Card().Rows {
			if r.Kind == connectcard.Fail {
				failText = r.Text
			}
		}
		if !strings.Contains(failText, c.want) || strings.Contains(failText, "hunter2") || strings.Contains(failText, "ftp") {
			t.Errorf("%q: error %q", c.url, failText)
		}
	}
	m := at(t, "url")
	m.SetValue("url", " http://10.0.0.5:8000/v1 ")
	m.SetValue("key", testKey)
	out, ok := press(t, &m, "connect").(ConnectURLMsg)
	if !ok || out.URL != "http://10.0.0.5:8000/v1" || out.Key != testKey || !m.Busy() {
		t.Fatalf("Connect sent %#v busy %v", out, m.Busy())
	}
	if strings.Contains(text(m), testKey) {
		t.Error("the key shows")
	}
}

// Spec: a local AI is looked for at once (no button); not running is one
// sentence with the install hint and Try again.
func TestLocal(t *testing.T) {
	m := newCard(nil)
	out, ok := pick(t, &m, ai.KindLMStudio).(DetectLocalMsg)
	if !ok || out.Kind != ai.KindLMStudio || !m.Busy() {
		t.Fatalf("picking LM Studio sent %#v", out)
	}
	if s := text(m); !strings.Contains(s, "Looking for LM Studio on this machine") {
		t.Errorf("card:\n%s", s)
	}
	m.Fail(m.Gen(), "LM Studio is not running on this machine.")
	s := text(m)
	if !strings.Contains(s, "not running") || !strings.Contains(s, "Try again") || !strings.Contains(s, "lmstudio.ai") {
		t.Errorf("card:\n%s", s)
	}
}

// Spec: ↑/↓ move through the list; ↓ on the last AI goes on to the
// buttons and ↑ comes back.
func TestListArrows(t *testing.T) {
	m := newCard(nil)
	for range Catalog() {
		do(t, &m, key(tea.KeyDown))
	}
	if m.card.FocusedID() != "" {
		t.Fatalf("↓ past the last AI: focus on %q, want the buttons", m.card.FocusedID())
	}
	do(t, &m, key(tea.KeyUp))
	do(t, &m, key(tea.KeyUp))
	do(t, &m, key(tea.KeyEnter))
	if m.Kind() != ai.KindLMStudio {
		t.Errorf("↑↑ from the buttons picks %q, want LM Studio", m.Kind())
	}
}

// Spec: a click on an AI in the list (or on a way) chooses it at once.
func TestClickChooses(t *testing.T) {
	m := newCard(nil)
	tg := m.OptionRects()
	if len(tg) != len(Catalog()) {
		t.Fatalf("%d option targets", len(tg))
	}
	for _, r := range tg {
		if r.ID == ai.KindOpenRouter {
			var cmd tea.Cmd
			m, cmd = m.HandleMouse(r.X+1, r.Y, tea.MouseActionPress, tea.MouseButtonLeft)
			if cmd != nil {
				t.Errorf("click sent %T", cmd())
			}
		}
	}
	if m.Step() != StepKey || !strings.Contains(text(m), "OpenRouter API key") {
		t.Fatalf("after the click: step %v\n%s", m.Step(), text(m))
	}
}
