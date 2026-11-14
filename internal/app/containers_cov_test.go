package app

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// Shared helpers of the COV-APP-CONTAINERS tests.

// ctrModel is the seeded Containers view (svcHostState) with the fake
// docker first on PATH and a temp HOME: every engine call hits the fake.
func ctrModel(t *testing.T) (*Model, *servicestest.Fake) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	f := hostFake(t)
	st := svcHostState()
	st.root = t.TempDir()
	mm := svcTestModel(st, 120, 50)
	return &mm, f
}

// ctrNvim attaches a real headless editor so terminal tabs open (the
// commands they run hit the fake docker on PATH).
func ctrNvim(t *testing.T, m *Model) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	t.Setenv("SHELL", "/bin/sh")
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(120, 40); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.ExecLua(`vim.o.shell = '/bin/sh'`)
	m.nvim = c
	m.terminalRows = 8
	m.output = newOutputStore()
	m.tasks = newTaskRunner()
	m.problems = newProblemsPanel()
}

func ctrToast(m *Model) string { return strings.Join(m.toast.View(200, 40), " ") }

// ctrGroupRow puts the cursor on the CONTAINERS group row of project.
func ctrGroupRow(t *testing.T, m *Model, project string) {
	t.Helper()
	for i, r := range m.services.rows() {
		if r.kind == svcRowGroup && strings.HasPrefix(r.section, "grp/containers/") && r.name == project {
			m.services.cursor = i
			return
		}
	}
	t.Fatalf("no group row %q:\n%s", project, svcRowsText(m.services))
}

// ctrKey sends one key to the Containers sidebar.
func ctrKey(m *Model, s string) tea.Cmd {
	next, cmd := m.handleServicesSidebarKey(keyMsg(s))
	*m = next
	return cmd
}

// ctrMsgs runs cmd and every Batch inside it, returning the messages
// (commands that block longer than the caller's patience are not used).
func ctrMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, ctrMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// ctrFind returns the first message of type T in msgs.
func ctrFind[T any](t *testing.T, msgs []tea.Msg) T {
	t.Helper()
	for _, m := range msgs {
		if v, ok := m.(T); ok {
			return v
		}
	}
	var zero T
	t.Fatalf("no %T in %#v", zero, msgs)
	return zero
}
