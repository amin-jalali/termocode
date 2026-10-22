package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// cmdWaits reports whether running cmd leaves a command that has not
// answered after wait: a timer or ticker (Bubble Tea ticks sleep).
func cmdWaits(cmd tea.Cmd, wait time.Duration) bool {
	if cmd == nil {
		return false
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if cmdWaits(c, wait) {
					return true
				}
			}
		}
		return false
	case <-time.After(wait):
		return true
	}
}

// Spec (U19 SS-7 perf guard): with the store unlocked and
// secret_lock_minutes 0 (never lock), the secret store runs no timer or
// ticker: after the unlock, idle messages and opening the store screen
// start nothing that waits.
func TestPerfGuardSecretsIdleNoTimers(t *testing.T) {
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	cfg := `{"ai_provider": "openai", "secret_lock_minutes": 0}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	m := secretsTestModel(t)
	s, err := configureSecretStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	if s.Locked() {
		t.Fatal("the store is locked")
	}
	m.toast = toast.New() // the unlock toast has its own expiry tick
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: m.w, Height: m.h},
		struct{}{},
	} {
		next, cmd := m.update(msg)
		*m = next
		m.toast = toast.New()
		if cmdWaits(cmd, 150*time.Millisecond) {
			t.Errorf("idle %T started a timer", msg)
		}
	}
	if cmd := m.openSecretsStore(); cmdWaits(cmd, 150*time.Millisecond) {
		t.Error("opening the store screen started a timer")
	}
	if s.Locked() {
		t.Error("an idle store locked itself with secret_lock_minutes 0")
	}
}

// Spec (U19 SS-7 perf guard): drawing the store screen does no file I/O
// and no decrypt. The screen is built once from the index when it opens
// (and works while locked, so no decrypt then either); with the store
// locked and secrets.json and secrets.age deleted, every later frame is
// byte-for-byte the same.
func TestPerfGuardSecretsStoreScreenRendersWithoutIO(t *testing.T) {
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	s, err := configureSecretStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("mcp.secret.GITHUB_PAT", "${env:GITHUB_TOKEN_SS7}"); err != nil {
		t.Fatal(err)
	}
	s.Lock()
	m.openSecretsStore()
	first := storeScreen(m)
	for _, want := range []string{"ai.key.openai", "mcp.secret.GITHUB_PAT", "${env:GITHUB_TOKEN_SS7}", "locked"} {
		if !strings.Contains(first, want) {
			t.Fatalf("the store screen lacks %q:\n%s", want, first)
		}
	}
	for _, f := range []string{secrets.IndexFile, secrets.AgeFile} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 50; i++ {
		if got := storeScreen(m); got != first {
			t.Fatalf("frame %d changed after the files were gone:\n%s", i, got)
		}
	}
	if !s.Locked() {
		t.Error("drawing unlocked the store")
	}
}

// Spec (U19 SS-8 perf guard): the idle lock is one timer at most. With
// secret_lock_minutes 0 an unlock and many uses arm none; with N > 0
// they arm exactly one, and idle messages and the store screen add none.
func TestPerfGuardSecretsLockTimerAtMostOne(t *testing.T) {
	for _, minutes := range []int{0, 30} {
		ticks := fakeLockTicks(t)
		dir, _ := withSecretSetting(t, "age", false)
		writeAgeStore(t, dir)
		setLockMinutes(t, dir, minutes)
		m := secretsTestModel(t)
		unlockThroughUI(t, m)
		s, _ := configureSecretStore()
		for i := 0; i < 200; i++ {
			if _, _, err := s.Get("ai.key.openai"); err != nil {
				t.Fatal(err)
			}
		}
		for _, msg := range []tea.Msg{tea.WindowSizeMsg{Width: m.w, Height: m.h}, struct{}{}} {
			next, cmd := m.update(msg)
			*m = next
			m.toast = toast.New()
			if cmdWaits(cmd, 150*time.Millisecond) {
				t.Errorf("%d min: idle %T started a timer", minutes, msg)
			}
		}
		if cmd := m.openSecretsStore(); cmdWaits(cmd, 150*time.Millisecond) {
			t.Errorf("%d min: the store screen started a timer", minutes)
		}
		want := 0
		if minutes > 0 {
			want = 1
		}
		if ticks.n != want {
			t.Errorf("%d min: %d lock timers, want %d", minutes, ticks.n, want)
		}
	}
}
