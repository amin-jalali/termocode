package app

import (
	"testing"
	"time"
)

// main opens the secret store in the background while the terminal is
// probed and the UI is built; the AI state then gets the same store
// without opening it again.
func TestWarmSecretStoreOpensTheStoreAhead(t *testing.T) {
	withConfigDir(t)
	secretStoreConfig.Lock()
	secretStoreConfig.key, secretStoreConfig.store, secretStoreConfig.err = "", nil, nil
	secretStoreConfig.Unlock()

	WarmSecretStore()()
	secretStoreConfig.Lock()
	warm := secretStoreConfig.store
	secretStoreConfig.Unlock()
	if warm == nil {
		t.Fatal("WarmSecretStore did not open the store")
	}
	if got, _ := configureSecretStore(); got != warm {
		t.Fatal("configureSecretStore opened the store again after the warm-up")
	}
}

// A keychain that never replies must not hold construction of the AI
// state (and thus the first frame) behind the warm-up mutex.
func TestWarmSecretStoreDoesNotBlockInitialAIState(t *testing.T) {
	withSecretSetting(t, "auto", false)
	entered, release := make(chan struct{}), make(chan struct{})
	secretKeyringProbe = func() bool { close(entered); <-release; return false }
	wait := WarmSecretStore()
	<-entered
	done := make(chan *aiState, 1)
	go func() { done <- newAIState() }()
	var state *aiState
	select {
	case state = <-done:
	case <-time.After(time.Second):
		close(release)
		wait()
		<-done
		t.Fatal("initial AI state waited for the keychain")
	}
	close(release)
	wait()
	if state == nil {
		t.Fatal("no initial AI state")
	}
	m := Model{ai: state}
	cmd := m.secretsWarmReadyCmd()
	if cmd == nil {
		t.Fatal("missing warm-up completion command")
	}
	m.onSecretsWarmReady(cmd().(secretsWarmReadyMsg))
	if state.provider == nil || state.warmPending != nil {
		t.Fatal("AI configuration was not loaded after warm-up")
	}
}
