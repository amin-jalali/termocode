package toast

import (
	"strings"
	"testing"
	"time"
)

func TestNewIsEmpty(t *testing.T) {
	m := New()
	if len(m.toasts) != 0 {
		t.Fatalf("new model has toasts: %v", m.toasts)
	}
	if m.nextID != 0 {
		t.Fatalf("nextID want 0, got %d", m.nextID)
	}
}

func TestPushAppendsAndAssignsID(t *testing.T) {
	m := New()
	m, cmd := m.Push(Info, "hello")
	if cmd == nil {
		t.Fatal("Push must schedule a tick")
	}
	if len(m.toasts) != 1 {
		t.Fatalf("toasts: want 1, got %d", len(m.toasts))
	}
	if m.toasts[0].ID != 1 {
		t.Fatalf("first ID want 1, got %d", m.toasts[0].ID)
	}
	if m.toasts[0].Severity != Info || m.toasts[0].Title != "hello" {
		t.Fatalf("toast payload mismatch: %+v", m.toasts[0])
	}
	m, _ = m.Push(Warn, "warn")
	if m.toasts[1].ID != 2 {
		t.Fatalf("second ID want 2, got %d", m.toasts[1].ID)
	}
}

// OnPush is a package-level hook; reset it between subtests so the suite
// stays order-independent.
func resetHook(t *testing.T) {
	t.Helper()
	old := OnPush
	t.Cleanup(func() { OnPush = old })
	OnPush = nil
}

func TestPushFiresOnPushHook(t *testing.T) {
	t.Run("err and warn fire", func(t *testing.T) {
		resetHook(t)
		var got []Severity
		var msgs []string
		OnPush = func(s Severity, msg string) {
			got = append(got, s)
			msgs = append(msgs, msg)
		}
		m := New()
		m, _ = m.Push(Errr, "boom")
		m, _ = m.Push(Warn, "careful")
		if len(got) != 2 {
			t.Fatalf("hook calls: want 2, got %d", len(got))
		}
		if got[0] != Errr || got[1] != Warn {
			t.Fatalf("severities: %v", got)
		}
		if msgs[0] != "boom" || msgs[1] != "careful" {
			t.Fatalf("messages: %v", msgs)
		}
	})

	t.Run("info also forwarded", func(t *testing.T) {
		// Hook spec: fired every push (the doc comment notes Errr/Warn use,
		// but the implementation forwards Info too). Verify the contract.
		resetHook(t)
		var n int
		OnPush = func(_ Severity, _ string) { n++ }
		m := New()
		m, _ = m.Push(Info, "info")
		if n != 1 {
			t.Fatalf("info push: hook calls want 1, got %d", n)
		}
	})

	t.Run("nil hook is safe", func(t *testing.T) {
		resetHook(t)
		OnPush = nil
		m := New()
		// Must not panic.
		_, _ = m.Push(Errr, "no hook")
	})
}

func TestTickRemovesExpired(t *testing.T) {
	m := New()
	m, _ = m.Push(Info, "stale")
	m, _ = m.Push(Info, "fresh")
	// Force the first toast into the past.
	m.toasts[0].ExpiresAt = time.Now().Add(-1 * time.Second)
	m, _ = m.Tick(time.Now())
	if len(m.toasts) != 1 {
		t.Fatalf("expired toast not removed: %+v", m.toasts)
	}
	if m.toasts[0].Title != "fresh" {
		t.Fatalf("wrong toast survived: %+v", m.toasts[0])
	}
}

func TestTickWithNoToastsReturnsNilCmd(t *testing.T) {
	m := New()
	_, cmd := m.Tick(time.Now())
	if cmd != nil {
		t.Fatal("empty Tick should not schedule another tick")
	}
}

func TestViewEmptyWhenNoToasts(t *testing.T) {
	m := New()
	if m.View(80, 24) != nil {
		t.Fatal("View with no toasts should return nil")
	}
}

func TestViewEmptyWhenZeroSize(t *testing.T) {
	m := New()
	m, _ = m.Push(Info, "x")
	if m.View(0, 24) != nil {
		t.Fatal("zero width must return nil")
	}
	if m.View(80, 0) != nil {
		t.Fatal("zero height must return nil")
	}
}

func TestViewRendersAtMostMaxStacked(t *testing.T) {
	m := New()
	for i := 0; i < maxStacked+2; i++ {
		m, _ = m.Push(Info, "msg")
	}
	lines := m.View(80, 24)
	want := maxStacked*rowsPerToast + (maxStacked-1)*gapBetweenToasts
	if len(lines) != want {
		t.Fatalf("stacked toasts: want %d rows, got %d", want, len(lines))
	}
	// At least one row across the block must contain the title text.
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "msg") {
		t.Fatalf("rendered block missing message: %q", joined)
	}
}

func TestViewRendersMessage(t *testing.T) {
	m := New()
	m, _ = m.Push(Errr, "saved!")
	lines := m.View(80, 24)
	if len(lines) != rowsPerToast {
		t.Fatalf("want %d rows, got %d", rowsPerToast, len(lines))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "saved!") {
		t.Fatalf("rendered block missing message: %q", lines)
	}
}
