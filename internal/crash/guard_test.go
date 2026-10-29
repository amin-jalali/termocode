package crash

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// nextNotice waits for one notice.
func nextNotice(t *testing.T) Notice {
	t.Helper()
	select {
	case n := <-Notices():
		return n
	case <-time.After(10 * time.Second):
		t.Fatal("no crash notice")
	}
	return Notice{}
}

func drainNotices() {
	for {
		select {
		case <-Notices():
		default:
			return
		}
	}
}

// TestGoRecoversAPanic: a panic in a goroutine started with Go does not
// end the process; the report is written and the UI is told.
func TestGoRecoversAPanic(t *testing.T) {
	useTempState(t)
	drainNotices()
	canary := "sk-" + "canary" + "0123456789abcdef"
	Go("git watcher", func() { panic("bad " + canary) })
	n := nextNotice(t)
	if n.Where != "git watcher" {
		t.Fatalf("notice where = %q", n.Where)
	}
	b, err := os.ReadFile(n.Report)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if !strings.Contains(string(b), "guard_test.go") {
		t.Errorf("report stack misses the panic site:\n%s", b)
	}
	if strings.Contains(string(b), canary) {
		t.Errorf("report holds the secret")
	}
}

func TestGoRunsFn(t *testing.T) {
	done := make(chan struct{})
	Go("x", func() { close(done) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("fn did not run")
	}
}

// TestPanicInHook: the test hook makes the named goroutine panic before
// its work; the others run as usual.
func TestPanicInHook(t *testing.T) {
	useTempState(t)
	drainNotices()
	undo := PanicIn("lsp reader")
	defer undo()
	var wg sync.WaitGroup
	ran := false
	wg.Add(1)
	Go("lsp reader", func() { ran = true })
	Go("other", func() { wg.Done() })
	wg.Wait()
	if n := nextNotice(t); n.Where != "lsp reader" {
		t.Fatalf("notice where = %q", n.Where)
	}
	if ran {
		t.Fatal("fn ran after the injected panic")
	}
}

type okMsg struct{}

// TestGuardCmd: a panicking command, also inside a batch or a sequence,
// returns no message instead of ending the program.
func TestGuardCmd(t *testing.T) {
	useTempState(t)
	drainNotices()
	boom := func() tea.Msg { panic("boom") }
	ok := func() tea.Msg { return okMsg{} }
	if msg := GuardCmd("cmd", boom)(); msg != nil {
		t.Fatalf("msg = %v", msg)
	}
	nextNotice(t)
	if GuardCmd("cmd", nil) != nil {
		t.Fatal("a nil cmd must stay nil")
	}
	if _, isOK := GuardCmd("cmd", ok)().(okMsg); !isOK {
		t.Fatal("a good cmd lost its message")
	}
	for _, wrapped := range []tea.Cmd{tea.Batch(boom, ok), tea.Sequence(boom, ok)} {
		msg := GuardCmd("cmd", wrapped)()
		cmds, isList := cmdList(msg)
		if !isList || len(cmds) != 2 {
			t.Fatalf("batch/sequence not kept: %T", msg)
		}
		if got := cmds[0](); got != nil {
			t.Fatalf("inner panic gave %v", got)
		}
		nextNotice(t)
		if _, isOK := cmds[1]().(okMsg); !isOK {
			t.Fatal("inner good cmd lost its message")
		}
	}
}

// TestGoWithCleansUp: after a recovered panic GoWith runs its cleanup
// (close a connection, end a session) after the report is written.
func TestGoWithCleansUp(t *testing.T) {
	useTempState(t)
	drainNotices()
	done := make(chan string, 1)
	GoWith("conn", func() { panic("x") }, func() { done <- "cleaned" })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cleanup did not run")
	}
	nextNotice(t)
	ran := make(chan bool, 1)
	GoWith("conn", func() { ran <- true }, func() { t.Error("cleanup ran without a panic") })
	<-ran
}
