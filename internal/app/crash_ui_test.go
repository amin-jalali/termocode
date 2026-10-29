package app

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
)

// crashToastText is the toasts' text, borders and line breaks gone.
func crashToastText(m Model) string {
	s := ansi.Strip(strings.Join(m.toast.View(200, 40), " "))
	s = strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// TestCrashNoticeShowsToast: a panic recovered in a background goroutine
// reaches the UI as an error toast; the app goes on and listens again.
func TestCrashNoticeShowsToast(t *testing.T) {
	m := aiTestModel(t)
	m.crash = newCrashUI()
	t.Cleanup(m.crash.close)
	crash.Go("the git watcher", func() { panic("test") })
	msg := m.crash.waitCmd()()
	if _, ok := msg.(crashNoticeMsg); !ok {
		t.Fatalf("waitCmd gave %T", msg)
	}
	nm, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("no command: the app stops listening for crash notices")
	}
	got := crashToastText(nm.(Model))
	for _, want := range []string{"Something went wrong in the git watcher", "your work is safe", "Report saved"} {
		if !strings.Contains(got, want) {
			t.Errorf("toast has no %q: %q", want, got)
		}
	}
}

// TestCrashWaitStopsOnClose: the listener ends when the model closes.
func TestCrashWaitStopsOnClose(t *testing.T) {
	c := newCrashUI()
	c.close()
	c.close() // twice is fine
	if msg := c.waitCmd()(); msg != nil {
		t.Fatalf("msg after close: %v", msg)
	}
}

// TestCrashReportUsesTheAppSecretMask: a secret the IDE knows (a
// password from the project's .env) never reaches a crash report.
func TestCrashReportUsesTheAppSecretMask(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	canary := "canary" + "-plain-" + "word-9271"
	ai.RegisterSecret(canary)
	path, err := crash.WriteReport("worker", "failed with "+canary, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), canary) {
		t.Fatalf("report holds the secret:\n%s", b)
	}
}
