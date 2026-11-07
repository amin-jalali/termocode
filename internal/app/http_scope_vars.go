package app

// `# @prompt name` values (typed before each send) and the global
// variables file (API-4).

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

const promptKindHTTPPromptVar promptKindEnum = 4512

// httpPromptWait is a request whose `# @prompt` values are being typed.
type httpPromptWait struct {
	step      httpChainStep
	skipChain bool
	next      int
	values    map[string]string
}

// httpAskPrompts asks the `# @prompt` values of step.req one by one,
// then sends; with none it sends at once.
func (m *Model) httpAskPrompts(step httpChainStep, skipChain bool) tea.Cmd {
	if len(step.req.Prompts) == 0 {
		return m.httpSendResolved(step.file, step.vars, step.req, step.env, skipChain, nil)
	}
	w := &httpPromptWait{step: step, skipChain: skipChain, values: map[string]string{}}
	m.httpS().promptWait = w
	return m.httpPromptNext(w)
}

func (m *Model) httpPromptNext(w *httpPromptWait) tea.Cmd {
	if w.next >= len(w.step.req.Prompts) {
		m.httpS().promptWait = nil
		return m.httpSendResolved(w.step.file, w.step.vars, w.step.req, w.step.env, w.skipChain, w.values)
	}
	p := w.step.req.Prompts[w.next]
	label := p.Label
	if label == "" {
		label = p.Name
	}
	m.openHTTPPrompt(promptKindHTTPPromptVar, "HTTP: "+req2label(w.step.req), label+":", "")
	m.prompt.SetAllowEmpty(true)
	if p.Secret() {
		m.prompt.SetMasked(true)
	}
	return nil
}

// httpPromptVarSubmit takes one typed value. ok=false: not ours.
func (m *Model) httpPromptVarSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindHTTPPromptVar {
		return nil, false
	}
	w := m.httpS().promptWait
	if w == nil || w.next >= len(w.step.req.Prompts) {
		return nil, true
	}
	w.values[w.step.req.Prompts[w.next].Name] = value
	w.next++
	return m.httpPromptNext(w), true
}

// httpOpenGlobals opens (and makes, 0600) the global variables file.
func (m *Model) httpOpenGlobals() tea.Cmd {
	p := httpGlobalsPath()
	if _, err := os.Stat(p); err != nil {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not create globals.json", err.Error())
		}
		if err := os.WriteFile(p, []byte(httpGlobalsTemplate), 0o600); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not create globals.json", err.Error())
		}
	}
	return m.httpOpenInEditor(p)
}

const httpGlobalsTemplate = "{\n  \"tenant\": \"demo\"\n}\n"
