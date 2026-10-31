//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SS-7: the secret store in the real binary, over the persistent session.
// The rig has no D-Bus and an SSH_CONNECTION, so the OS keyring is never
// tried and secret_store "auto" picks the encrypted file. The scrypt work
// factor stays at its default (about a second), so waits are generous.

const (
	secretsPass   = "e2e-test-passphrase-123"
	secretsAIKey  = "sk-e2e-FAKE-key-7d3f91c2"
	secretsWait   = 30 * time.Second
	secretsPrompt = "Secrets passphrase:"
)

func (r *sessionRig) configDir() string { return filepath.Join(r.home, ".config", "termocode") }

// secretFiles are where the store keeps things.
func (r *sessionRig) ageFile() string   { return filepath.Join(r.configDir(), "secrets.age") }
func (r *sessionRig) indexFile() string { return filepath.Join(r.configDir(), "secrets.json") }
func (r *sessionRig) plainFile() string {
	return filepath.Join(r.configDir(), "ai", "credentials.json")
}

// typeMasked answers a masked prompt that shows label.
func (tm *Term) typeMasked(label, text string) {
	tm.t.Helper()
	tm.WaitText(label, secretsWait)
	tm.settle()
	tm.Type(text)
	tm.Send(KeyEnter)
}

// noSecretOnScreen fails if any secret string is visible.
func (tm *Term) noSecretOnScreen(secrets ...string) {
	tm.t.Helper()
	for _, s := range secrets {
		if tm.Has(s) {
			tm.t.Fatalf("secret %q is on screen:\n%s", s, tm.Screen())
		}
	}
}

// startAIKeyPrompt runs AI: Configure Provider… and picks a provider.
func (tm *Term) startAIKeyPrompt(kind string) {
	tm.t.Helper()
	tm.Palette("AI: Configure Provider")
	tm.WaitText("AI Provider", uiTimeout)
	tm.Type(kind)
	tm.settle()
	tm.Send(KeyEnter)
}

// createStore makes the encrypted store through the AI key prompt: a new
// passphrase twice, then the key itself.
func (r *sessionRig) createStore(tm *Term) {
	tm.t.Helper()
	tm.startAIKeyPrompt("OpenAI")
	tm.typeMasked(secretsPrompt, secretsPass)
	tm.typeMasked("Type it again:", secretsPass)
	tm.WaitText("OpenAI API key", secretsWait)
	tm.settle()
	tm.Type(secretsAIKey)
	tm.noSecretOnScreen(secretsAIKey)
	tm.Send(KeyEnter)
	tm.WaitText("AI Model", secretsWait)
	tm.Send(KeyEsc)
	tm.WaitGone("AI Model", uiTimeout)
}

// restartServer ends the session server (the store is locked in the new
// one) and starts a client for a new server.
func (r *sessionRig) restartServer(old *Term) *Term {
	r.t.Helper()
	if out, err := r.output("kill"); err != nil {
		r.t.Fatalf("kill: %v\n%s", err, out)
	}
	select {
	case <-old.exited:
	case <-time.After(10 * time.Second):
		r.t.Fatalf("client still running after kill")
	}
	r.waitSocket(false)
	return r.start()
}

// openStoreScreen runs AI: Secret Store….
func (tm *Term) openStoreScreen() {
	tm.t.Helper()
	tm.Palette("AI: Secret Store")
	tm.WaitText("Backend: encrypted file", secretsWait)
	tm.settle()
}

func readFileOrEmpty(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// Spec: a first key on a host without a keyring asks for a new passphrase
// twice and saves the key in secrets.age; the key is nowhere in clear on
// disk, and the index holds the name only.
func TestSecretsFirstKeyCreatesEncryptedStore(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	r.createStore(tm)

	age, err := os.ReadFile(r.ageFile())
	if err != nil {
		t.Fatalf("secrets.age: %v", err)
	}
	if strings.Contains(string(age), secretsAIKey) || strings.Contains(string(age), "ai.key.openai") {
		t.Error("secrets.age holds the key or its name in clear")
	}
	idx := readFileOrEmpty(r.indexFile())
	if !strings.Contains(idx, "ai.key.openai") {
		t.Errorf("secrets.json does not name the key:\n%s", idx)
	}
	if strings.Contains(idx, secretsAIKey) {
		t.Errorf("secrets.json holds the value:\n%s", idx)
	}
	if _, err := os.Stat(r.plainFile()); err == nil {
		t.Error("a plain credentials.json was written")
	}
}

// Spec: the passphrase lives as long as the UI process: a reconnect does
// not ask again; a new server starts locked and asks on the first need.
func TestSecretsPassphraseOncePerUIProcess(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	r.createStore(tm)

	tm.Disconnect()
	tm2 := r.start()
	tm2.openStoreScreen()
	tm2.WaitText("State: unlocked", uiTimeout)
	tm2.WaitText("ai.key.openai", uiTimeout)
	tm2.noSecretOnScreen(secretsAIKey, secretsPass)
	if tm2.Has(secretsPrompt) {
		t.Fatalf("a reconnect asked for the passphrase:\n%s", tm2.Screen())
	}
	tm2.Send(KeyEsc)
	tm2.WaitGone("Secret Store", uiTimeout)

	tm3 := r.restartServer(tm2)
	tm3.openStoreScreen()
	tm3.WaitText("State: locked", uiTimeout)
	tm3.Send(KeyEsc)
	tm3.WaitGone("Secret Store", uiTimeout)
	tm3.startAIKeyPrompt("OpenAI")
	tm3.WaitText(secretsPrompt, secretsWait)
	tm3.noSecretOnScreen("Type it again")
	tm3.typeMasked(secretsPrompt, secretsPass)
	tm3.WaitText("OpenAI API key", secretsWait)
}

// Spec: three wrong passphrases leave the store locked and say how to
// unlock it; AI: Unlock Secrets then works.
func TestSecretsWrongPassphraseThenUnlockFromPalette(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	r.createStore(tm)
	tm = r.restartServer(tm)

	tm.startAIKeyPrompt("OpenAI")
	tm.typeMasked(secretsPrompt, "wrong-pass-0001")
	tm.WaitText("Wrong passphrase (2 tries left)", secretsWait)
	tm.typeMasked(secretsPrompt, "wrong-pass-0002")
	tm.WaitText("Wrong passphrase (1 try left)", secretsWait)
	tm.typeMasked(secretsPrompt, "wrong-pass-0003")
	tm.WaitText("Secrets stay locked", secretsWait)
	tm.WaitText("AI: Unlock Secrets", uiTimeout)
	tm.WaitGone(secretsPrompt, uiTimeout)

	tm.openStoreScreen()
	tm.WaitText("State: locked", uiTimeout)
	tm.Send(KeyEsc)
	tm.WaitGone("Secret Store", uiTimeout)

	tm.Palette("AI: Unlock Secrets")
	tm.typeMasked(secretsPrompt, secretsPass)
	tm.WaitText("Secrets unlocked", secretsWait)
	tm.openStoreScreen()
	tm.WaitText("State: unlocked", uiTimeout)
}

// Spec: a plain ai/credentials.json starts the migration card. It counts
// what it found and never shows a value. Encrypted file: passphrase twice;
// a card then asks before the plain file goes (Keep for now first: Enter
// keeps it). Delete the plain file replaces it by credentials.json.migrated,
// and the key is in the store after a restart and an unlock.
func TestSecretsMigrationCardMovesPlainFile(t *testing.T) {
	r := newSessionRig(t)
	if err := os.MkdirAll(filepath.Dir(r.plainFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	plain := `{"keys":{"openai":"` + secretsAIKey + `"}}`
	if err := os.WriteFile(r.plainFile(), []byte(plain), 0o600); err != nil {
		t.Fatal(err)
	}
	tm := r.start()
	tm.WaitText("Move your secrets to a safer store", secretsWait)
	tm.WaitText("1 AI key", uiTimeout)
	tm.WaitText("Encrypted file", uiTimeout)
	tm.noSecretOnScreen(secretsAIKey)
	tm.settle()
	tm.Send(KeyEnter) // Encrypted file is the recommended (first) button
	tm.typeMasked(secretsPrompt, secretsPass)
	tm.typeMasked("Type it again:", secretsPass)
	tm.WaitText("Secrets moved", secretsWait)

	// The plain file stays until the user says so: Keep for now is first
	// and focused, so Enter keeps it.
	tm.WaitText("Delete the old plain file?", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Plain file kept", uiTimeout)
	if !strings.Contains(readFileOrEmpty(r.plainFile()), secretsAIKey) {
		t.Fatal("Keep for now removed credentials.json")
	}
	// AI: Move Secrets… asks again; Delete the plain file removes it.
	tm.Palette("AI: Move Secrets to a Safer Store…")
	tm.WaitText("Delete the old plain file?", uiTimeout)
	tm.settle()
	tm.Send("\x1b[C") // Right: Delete the plain file
	tm.settle()
	tm.Send(KeyEnter)
	waitFile(t, r.plainFile(), false, "Delete the plain file left credentials.json")
	if _, err := os.Stat(r.plainFile() + ".migrated"); err != nil {
		t.Errorf("credentials.json.migrated: %v", err)
	}
	if strings.Contains(readFileOrEmpty(r.plainFile()+".migrated"), secretsAIKey) {
		t.Error("the marker file holds the key")
	}
	if strings.Contains(readFileOrEmpty(r.ageFile()), secretsAIKey) {
		t.Error("secrets.age holds the key in clear")
	}

	tm = r.restartServer(tm)
	tm.WaitGone("Move your secrets", uiTimeout)
	tm.Palette("AI: Unlock Secrets")
	tm.typeMasked(secretsPrompt, secretsPass)
	tm.WaitText("Secrets unlocked", secretsWait)
	tm.openStoreScreen()
	tm.WaitText("ai.key.openai", uiTimeout)
	tm.noSecretOnScreen(secretsAIKey)
}

// Spec: the store screen lists names and kinds, never values; a reference
// shows as typed.
func TestSecretsStoreScreenShowsNamesNeverValues(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	r.createStore(tm)
	// A reference in the key prompt is stored as a reference.
	tm.startAIKeyPrompt("OpenRouter")
	tm.WaitText("OpenRouter API key", secretsWait)
	tm.Type("${env:E2E_OPENROUTER_KEY}")
	tm.Send(KeyEnter)
	tm.WaitText("AI Model", secretsWait)
	tm.Send(KeyEsc)
	tm.WaitGone("AI Model", uiTimeout)

	tm.openStoreScreen()
	tm.WaitText("ai.key.openai", uiTimeout)
	tm.WaitText("ai.key.openrouter", uiTimeout)
	tm.WaitText("${env:E2E_OPENROUTER_KEY}", uiTimeout)
	tm.noSecretOnScreen(secretsAIKey, secretsPass)
	tm.CheckScreen("secret store screen", mainFrame)
}
