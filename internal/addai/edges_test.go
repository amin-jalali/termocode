package addai

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clilogin"
)

// The Replace question names how the AI is connected now, whatever way
// providers.json says.
func TestReplaceQuestionNamesTheWayNow(t *testing.T) {
	for method, want := range map[string]string{
		ai.MethodCLI:   "a sign-in via Claude Code",
		ai.MethodLocal: "a server on this machine",
		ai.MethodURL:   "a server URL",
	} {
		conn := map[string]ai.ProviderEntry{ai.KindAnthropic: {Kind: ai.KindAnthropic, Method: method, CLI: ai.CLIClaude}}
		m := newCard(conn)
		pick(t, &m, ai.KindAnthropic)
		do(t, &m, key(tea.KeyDown)) // the API key way
		do(t, &m, key(tea.KeyEnter))
		if m.Step() != StepConfirm {
			t.Fatalf("%s: step %v", method, m.Step())
		}
		s := text(m)
		if !strings.Contains(s, want) || !strings.Contains(s, "the API key") {
			t.Errorf("%s: question\n%s", method, s)
		}
		// Back from the question goes to the ways, nothing changes.
		press(t, &m, "back")
		if m.Step() != StepMethod {
			t.Errorf("%s: back went to %v", method, m.Step())
		}
	}
}

// From the sign-in card, "Use an API key" on an AI signed in by its CLI
// asks first; Keep then goes to the ways, not back into the sign-in.
func TestAPIKeyFromSignInAsksThenKeepGoesToWays(t *testing.T) {
	conn := map[string]ai.ProviderEntry{ai.KindOpenAI: {Kind: ai.KindOpenAI, Method: ai.MethodCLI, CLI: ai.CLICodex}}
	m := newCard(conn)
	pick(t, &m, ai.KindOpenAI)
	do(t, &m, key(tea.KeyEnter)) // the sign-in way: same as now, no question
	if m.Step() != StepSignIn {
		t.Fatalf("step %v", m.Step())
	}
	press(t, &m, "apikey")
	if m.Step() != StepConfirm {
		t.Fatalf("api key: step %v", m.Step())
	}
	press(t, &m, "keep")
	if m.Step() != StepMethod {
		t.Errorf("keep went to %v, want the ways", m.Step())
	}
	if m.Method() != ai.MethodCLI {
		t.Errorf("method %q", m.Method())
	}
}

func TestCopyLinkOnlyWithALink(t *testing.T) {
	m, _ := signInCard(t, true)
	if out := press(t, &m, "copy"); out != nil {
		t.Errorf("copy without a link sent %T", out)
	}
	m.Progress(m.Gen(), clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: testLink})
	if out, ok := press(t, &m, "copy").(CopyLinkMsg); !ok || out.Gen != m.Gen() {
		t.Errorf("copy sent %#v", out)
	}
	// Ready is the app's to act on: the card stays as it is.
	before := text(m)
	if !m.Progress(m.Gen(), clilogin.State{Phase: clilogin.PhaseReady}) || text(m) != before {
		t.Error("ready changed the card")
	}
	m.Fail(m.Gen(), "stopped")
	if m.SignInURL() != "" {
		t.Error("an ended sign-in still has a link")
	}
}

func TestDoneCardModelCounts(t *testing.T) {
	m := at(t, "local")
	m.Finish(m.Gen(), nil)
	if s := text(m); !strings.Contains(s, "No models yet") || !strings.Contains(s, "0 models") {
		t.Errorf("no models:\n%s", s)
	}
	var many []string
	for i := 0; i < maxChips+3; i++ {
		many = append(many, fmt.Sprintf("m%02d", i))
	}
	m = at(t, "local")
	m.Finish(m.Gen(), many)
	if s := text(m); !strings.Contains(s, "and 3 more") || strings.Contains(s, fmt.Sprintf("m%02d", maxChips)) {
		t.Errorf("many models:\n%s", s)
	}
	// A server URL has no "with …" part.
	m = at(t, "url")
	m.SetValue("url", "http://10.0.0.5:8000/v1")
	press(t, &m, "connect")
	m.Finish(m.Gen(), []string{"x"})
	if s := text(m); !strings.Contains(s, "Connected · 1 model") {
		t.Errorf("url done:\n%s", s)
	}
}

func TestOptionRectsOnlyOnLists(t *testing.T) {
	if got := at(t, "key").OptionRects(); got != nil {
		t.Errorf("key step has options %v", got)
	}
	m := at(t, "method")
	rs := m.OptionRects()
	if len(rs) != 2 || rs[0].ID != ai.MethodCLI || rs[1].ID != ai.MethodKey {
		t.Errorf("method options %+v", rs)
	}
	x, y := m.Origin()
	if x < 0 || y < 0 || rs[0].Y <= y {
		t.Errorf("origin %d,%d rect %+v", x, y, rs[0])
	}
}

func TestUpdateIgnoresOtherMessages(t *testing.T) {
	m := at(t, "key")
	if out := do(t, &m, tea.WindowSizeMsg{Width: 10, Height: 10}); out != nil || m.Step() != StepKey {
		t.Errorf("sent %T, step %v", out, m.Step())
	}
}

func TestClamp(t *testing.T) {
	for _, c := range [][3]int{{-1, 3, 0}, {5, 3, 2}, {1, 0, 0}, {1, 3, 1}} {
		if got := clamp(c[0], c[1]); got != c[2] {
			t.Errorf("clamp(%d, %d) = %d", c[0], c[1], got)
		}
	}
}
