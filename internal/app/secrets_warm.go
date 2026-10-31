package app

import (
	"github.com/amin-jalali/termocode/internal/crash"
	tea "github.com/charmbracelet/bubbletea"
	"sync"
)

var secretWarm struct {
	sync.Mutex
	done chan struct{}
}

func secretStoreWarmPending() <-chan struct{} {
	secretWarm.Lock()
	defer secretWarm.Unlock()
	if secretWarm.done == nil {
		return nil
	}
	select {
	case <-secretWarm.done:
		return nil
	default:
		return secretWarm.done
	}
}

type secretsWarmReadyMsg struct {
	state *aiState
	done  <-chan struct{}
}

func (m Model) secretsWarmReadyCmd() tea.Cmd {
	if m.ai == nil || m.ai.warmPending == nil {
		return nil
	}
	state, done := m.ai, m.ai.warmPending
	return func() tea.Msg { <-done; return secretsWarmReadyMsg{state, done} }
}

func (m *Model) onSecretsWarmReady(msg secretsWarmReadyMsg) tea.Cmd {
	if m.ai != msg.state || m.ai.warmPending != msg.done {
		return nil
	}
	m.ai.reload()
	return m.aiProfileErrorsCmd()
}

// WarmSecretStore opens the secret store in the background, so the AI
// state built in New finds it open. The first start writes the store's
// index file (fsync, a few ms) and on a desktop may ask the OS keyring;
// main calls this before it probes the terminal, so that time overlaps
// the rest of startup. The returned func waits until it is open.
func WarmSecretStore() (wait func()) {
	done := make(chan struct{})
	secretWarm.Lock()
	secretWarm.done = done
	secretWarm.Unlock()
	crash.Go("opening the secret store", func() {
		defer close(done) // also after a panic, so the wait never hangs
		_, _ = configureSecretStore()
	})
	return func() { <-done }
}
