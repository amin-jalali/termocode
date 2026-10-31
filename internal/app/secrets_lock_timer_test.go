package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zalando/go-keyring"

	"github.com/amin-jalali/termocode/internal/clock"
)

// lockTicks records the lock timers the app arms (secretsLockTick). The
// fake arms nothing that sleeps: a test delivers the tick itself.
type lockTicks struct {
	n    int           // timers armed
	last time.Duration // the delay of the last one
	gen  int           // the generation of the last one
}

func fakeLockTicks(t *testing.T) *lockTicks {
	t.Helper()
	r := &lockTicks{}
	old := secretsLockTick
	secretsLockTick = func(d time.Duration, gen int) tea.Cmd {
		r.n++
		r.last, r.gen = d, gen
		return nil
	}
	t.Cleanup(func() { secretsLockTick = old })
	return r
}

// fakeNow freezes clock.Now; the returned func moves it on.
func fakeNow(t *testing.T) (advance func(time.Duration)) {
	t.Helper()
	// A leftover timer of another test (an MCP reconnect) may read the
	// clock from its own goroutine: guard now.
	var mu sync.Mutex
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	restore := clock.Set(func() time.Time { mu.Lock(); defer mu.Unlock(); return now })
	t.Cleanup(restore)
	return func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
}

// setLockMinutes writes secret_lock_minutes into config.json, keeping
// the other keys.
func setLockMinutes(t *testing.T, dir string, n int) {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	cfg := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	cfg["secret_lock_minutes"] = n
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// unlockThroughUI opens the passphrase prompt and types the passphrase,
// as the user does.
func unlockThroughUI(t *testing.T, m *Model) {
	t.Helper()
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-unlock"))
	submitSecrets(t, m, secretsTestPass)
	if storeLocked(t) {
		t.Fatal("the unlock failed")
	}
}

func deliverLockTick(m *Model, gen int) tea.Cmd {
	next, cmd := m.update(secretsLockTickMsg{gen: gen})
	*m = next
	return cmd
}

// Spec (U19 SS-8): with secret_lock_minutes 0 the unlock arms no timer.
func TestSecretsLockTimerNoneWhenZero(t *testing.T) {
	ticks := fakeLockTicks(t)
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	setLockMinutes(t, dir, 0)
	m := secretsTestModel(t)
	unlockThroughUI(t, m)
	if ticks.n != 0 {
		t.Fatalf("%d lock timers with secret_lock_minutes 0", ticks.n)
	}
}

// Spec (U19 SS-8): with N > 0 the unlock arms one timer of N minutes;
// many uses arm no more; after N idle minutes the tick locks the store
// with a quiet toast.
func TestSecretsLockTimerLocksAfterIdle(t *testing.T) {
	ticks := fakeLockTicks(t)
	advance := fakeNow(t)
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	setLockMinutes(t, dir, 5)
	m := secretsTestModel(t)
	unlockThroughUI(t, m)
	if ticks.n != 1 || ticks.last != 5*time.Minute {
		t.Fatalf("after unlock: %d timers, last %v; want 1 of 5m", ticks.n, ticks.last)
	}
	s, _ := configureSecretStore()
	for i := 0; i < 100; i++ {
		if _, _, err := s.Get("ai.key.openai"); err != nil {
			t.Fatal(err)
		}
	}
	if ticks.n != 1 {
		t.Fatalf("uses armed timers: %d", ticks.n)
	}
	advance(5 * time.Minute)
	deliverLockTick(m, ticks.gen)
	if !storeLocked(t) {
		t.Fatal("not locked after 5 idle minutes")
	}
	if got := toastText(*m); !strings.Contains(got, "Secrets locked after 5 min idle") {
		t.Fatalf("toast %q", got)
	}
	if ticks.n != 1 {
		t.Fatalf("a locked store armed another timer (%d)", ticks.n)
	}
	if strings.Contains(toastText(*m), secretsTestPass) {
		t.Fatal("the toast shows the passphrase")
	}
}

// Spec (U19 SS-8): a use restarts the idle count. The tick that comes
// early re-arms once for the rest of the time instead of locking.
func TestSecretsLockTimerUseResets(t *testing.T) {
	ticks := fakeLockTicks(t)
	advance := fakeNow(t)
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	setLockMinutes(t, dir, 5)
	m := secretsTestModel(t)
	unlockThroughUI(t, m)
	s, _ := configureSecretStore()
	advance(4 * time.Minute)
	if _, _, err := s.Get("ai.key.openai"); err != nil {
		t.Fatal(err)
	}
	advance(1 * time.Minute) // the first tick: 5 min after unlock, 1 after the use
	deliverLockTick(m, ticks.gen)
	if storeLocked(t) {
		t.Fatal("locked although used a minute ago")
	}
	if ticks.n != 2 || ticks.last != 4*time.Minute {
		t.Fatalf("re-arm: %d timers, last %v; want 2, last 4m", ticks.n, ticks.last)
	}
	advance(4 * time.Minute)
	deliverLockTick(m, ticks.gen)
	if !storeLocked(t) {
		t.Fatal("not locked 5 min after the last use")
	}
}

// Spec (U19 SS-8): a tick from before a manual lock does nothing, even
// after a new unlock (its generation is old).
func TestSecretsLockTimerStaleTickIgnored(t *testing.T) {
	ticks := fakeLockTicks(t)
	advance := fakeNow(t)
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	setLockMinutes(t, dir, 5)
	m := secretsTestModel(t)
	unlockThroughUI(t, m)
	old := ticks.gen
	m.dispatchPaletteAction("secrets-lock")
	unlockThroughUI(t, m)
	if ticks.n != 2 || ticks.gen == old {
		t.Fatalf("relock: %d timers, gen %d (old %d)", ticks.n, ticks.gen, old)
	}
	advance(10 * time.Minute)
	deliverLockTick(m, old)
	if storeLocked(t) {
		t.Fatal("an old tick locked the store")
	}
	if ticks.n != 2 {
		t.Fatalf("an old tick armed a timer (%d)", ticks.n)
	}
}

// Spec (U19 SS-8): the keyring and the plain file have no passphrase:
// no timer for them whatever the setting.
func TestSecretsLockTimerNotForKeyringOrPlain(t *testing.T) {
	keyring.MockInit()
	for _, backend := range []string{"plain", "keyring"} {
		ticks := fakeLockTicks(t)
		dir, _ := withSecretSetting(t, backend, true)
		setLockMinutes(t, dir, 5)
		m := secretsTestModel(t)
		if s, err := configureSecretStore(); err != nil || s.Backend() != backend {
			t.Fatalf("%s: store %v %v", backend, s, err)
		}
		if cmd := m.secretsArmLock(); cmd != nil || ticks.n != 0 {
			t.Fatalf("%s store armed %d timers", backend, ticks.n)
		}
	}
}

// fakeNow may be read from another goroutine while the test moves it on
// (a leftover timer of another test reads the clock): no data race.
func TestFakeNowIsSafeAcrossGoroutines(t *testing.T) {
	advance := fakeNow(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = clock.Now()
		}
	}()
	for i := 0; i < 100; i++ {
		advance(time.Second)
	}
	<-done
}
