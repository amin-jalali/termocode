package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
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

// flatCard is the card's text, borders and line breaks gone.
func flatCard(m Model) string {
	s := strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(ansi.Strip(m.confirm.View()))
	return strings.Join(strings.Fields(s), " ")
}

// pendingCrash writes a report and marks it as the crash of the last run.
func pendingCrash(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, err := crash.WriteReport("update", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := crash.MarkFatal(path); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCrashCardNextStart: after a crash the next start shows a card with
// Close first (focused), Show report and Copy report path; it is shown
// once.
func TestCrashCardNextStart(t *testing.T) {
	m := aiTestModel(t)
	pendingCrash(t)
	m.crash = newCrashUI()
	t.Cleanup(m.crash.close)
	cmd := m.crashCardCmd()
	if cmd == nil {
		t.Fatal("no card after a crash")
	}
	if m.crashCardCmd() != nil {
		t.Fatal("card queued twice")
	}
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	if !m.confirmOpen || m.confirmKind != confirmKindCrash {
		t.Fatal("card not open")
	}
	view := flatCard(m)
	for _, want := range []string{"termocode stopped unexpectedly last time.", "Close", "Show report", "Copy report path"} {
		if !strings.Contains(view, want) {
			t.Errorf("card has no %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "Close") > strings.Index(view, "Show report") {
		t.Errorf("Close is not the first button:\n%s", view)
	}
	if b := m.confirm.Buttons(); len(b) != 3 || b[0].ID != "close" || b[0].Style == confirm.StyleDestructive {
		t.Errorf("buttons %+v, want Close first", b)
	}
	if _, c := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter}); c == nil {
		t.Error("Enter picks nothing")
	} else if sel, ok := c().(confirm.SelectMsg); !ok || sel.ID != "close" {
		t.Errorf("Enter picks %v, want close", c())
	}
	if crash.Pending() != "" {
		t.Error("the crash is still pending after the card was shown")
	}
	if newCrashUI().report != "" {
		t.Error("the next start would show the card again")
	}
}

// TestCrashCardSaysRestored: when the unsaved work came back, the card
// says so.
func TestCrashCardSaysRestored(t *testing.T) {
	m := aiTestModel(t)
	pendingCrash(t)
	m.crash = newCrashUI()
	t.Cleanup(m.crash.close)
	m.crash.restored = 2
	nm, _ := m.Update(m.crashCardCmd()())
	m = nm.(Model)
	if v := flatCard(m); !strings.Contains(v, "Your unsaved work was restored.") {
		t.Errorf("card does not say restored:\n%s", v)
	}
}

// TestCrashCardWaitsForOtherDialog: a dialog already open stays; the card
// comes once it closes.
func TestCrashCardWaitsForOtherDialog(t *testing.T) {
	m := aiTestModel(t)
	pendingCrash(t)
	m.crash = newCrashUI()
	t.Cleanup(m.crash.close)
	m.confirmOpen, m.confirmKind = true, confirmKindQuit
	nm, _ := m.Update(m.crashCardCmd()())
	m = nm.(Model)
	if m.confirmKind != confirmKindQuit {
		t.Fatal("card replaced the open dialog")
	}
	m.confirmOpen = false
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m = nm.(Model); !m.confirmOpen || m.confirmKind != confirmKindCrash {
		t.Fatal("card not shown after the dialog closed")
	}
}

// TestCrashCardCopyPath: Copy report path puts the path on the clipboard.
func TestCrashCardCopyPath(t *testing.T) {
	m := aiTestModel(t)
	report := pendingCrash(t)
	m.crash = newCrashUI()
	t.Cleanup(m.crash.close)
	var got string
	defer SetClipboardSink(func(s string) { got = s })()
	nm, _ := m.Update(m.crashCardCmd()())
	m = nm.(Model)
	m.handleConfirmSelect("copy")
	if got != report {
		t.Fatalf("copied %q, want %q", got, report)
	}
}
