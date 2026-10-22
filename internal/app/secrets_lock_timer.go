package app

import (
	"strconv"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── The idle lock: secret_lock_minutes (U19 SS-8) ──────────────────────
//
// With secret_lock_minutes N > 0 and the encrypted file unlocked, ONE
// one-shot timer (tea.Tick) runs. A secret use does not touch the timer:
// it only stores the time (secretsTouch, an atomic write, from any
// goroutine). When the timer fires it compares that time with N: idle
// long enough → Lock and a quiet toast; used since → one new timer for
// the rest of the time. So there is never more than one timer, and none
// at all with N = 0, while locked, or for the keyring and the plain file
// (0 % idle rule). Each timer carries a generation; a manual lock or a
// new unlock bumps it, so an older tick that still arrives does nothing.

// secretsLastUse is the clock.Now (UnixNano) of the last secret use.
var secretsLastUse atomic.Int64

// secretsTouch is the store's OnUse hook: a use restarts the idle count.
func secretsTouch() { secretsLastUse.Store(clock.Now().UnixNano()) }

// secretsLockTickMsg is the lock timer firing.
type secretsLockTickMsg struct{ gen int }

// secretsLockTick arms the one-shot timer; tests swap it so nothing
// sleeps.
var secretsLockTick = func(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return secretsLockTickMsg{gen: gen} })
}

// secretLockMinutes is secret_lock_minutes (0 = never).
func secretLockMinutes() int {
	if v := loadSettings().SecretLockMinutes; v != nil && *v > 0 {
		return *v
	}
	return 0
}

// secretsLockApplies reports an unlocked encrypted file and a timeout.
func (m *Model) secretsLockApplies() (minutes int, ok bool) {
	s, err := m.secretStore()
	if err != nil || s == nil || s.Backend() != "age" || s.Locked() {
		return 0, false
	}
	n := secretLockMinutes()
	return n, n > 0
}

// secretsArmLock starts the lock timer after an unlock. A timer that
// already runs is kept (it re-arms itself for the rest of the time).
func (m *Model) secretsArmLock() tea.Cmd {
	st := m.secretsS()
	if st.lockArmed {
		return nil
	}
	n, ok := m.secretsLockApplies()
	if !ok {
		return nil
	}
	secretsTouch()
	st.lockGen++
	st.lockArmed = true
	return secretsLockTick(time.Duration(n)*time.Minute, st.lockGen)
}

// secretsDisarmLock forgets the running timer: its tick will do nothing.
func (m *Model) secretsDisarmLock() {
	st := m.secretsS()
	st.lockGen++
	st.lockArmed = false
}

// onSecretsLockTick locks after N idle minutes or re-arms for the rest.
func (m *Model) onSecretsLockTick(msg secretsLockTickMsg) tea.Cmd {
	st := m.secretsS()
	if msg.gen != st.lockGen || !st.lockArmed {
		return nil // an older timer
	}
	st.lockArmed = false
	n, ok := m.secretsLockApplies()
	if !ok {
		return nil // locked by hand, another store, or the setting is 0 now
	}
	limit := time.Duration(n) * time.Minute
	idle := clock.Since(time.Unix(0, secretsLastUse.Load()))
	if idle < limit {
		st.lockArmed = true
		return secretsLockTick(limit-idle, st.lockGen)
	}
	s, _ := m.secretStore()
	s.Lock()
	st.asked, st.tries = false, 0
	m.secretsChanged()
	return m.secretsToast(toast.Info, "Secrets locked after "+strconv.Itoa(n)+" min idle", "F1 → AI: Unlock Secrets to use them again.")
}
