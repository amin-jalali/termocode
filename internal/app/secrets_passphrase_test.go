package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/zalando/go-keyring"

	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
)

const secretsNewPass = "a brand new longer passphrase"

// ageOpensWith reports whether pass opens secrets.age in dir.
func ageOpensWith(t *testing.T, dir, pass string) bool {
	t.Helper()
	s, err := secrets.Open(secrets.Options{Dir: dir, Backend: "age", ScryptWorkFactor: 14})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Unlock(pass)
	s.Lock()
	if err != nil && !errors.Is(err, secrets.ErrWrongPassphrase) {
		t.Fatalf("open: %v", err)
	}
	return err == nil
}

// promptNote is the open prompt as plain text (title, label, note).
func promptNote(m *Model) string { return ansi.Strip(m.prompt.Box()) }

// Spec (U19 SS-8): AI: Change Secrets Passphrase… asks the current
// passphrase, then the new one twice, re-encrypts the file and keeps
// the store open. No passphrase reaches a toast or errors.log.
func TestSecretsChangePassphraseFlow(t *testing.T) {
	ticks := fakeLockTicks(t)
	m, s, dir := unlockedAgeStore(t)
	setLockMinutes(t, dir, 5)
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-passphrase"))
	if !m.secretsPromptOpen() || !m.prompt.Masked() || !strings.Contains(promptNote(m), "Current passphrase") {
		t.Fatalf("no masked prompt for the current passphrase:\n%s", promptNote(m))
	}
	submitSecrets(t, m, secretsTestPass)
	if !m.secretsPromptOpen() || !strings.Contains(promptNote(m), "New passphrase") {
		t.Fatalf("no prompt for the new passphrase:\n%s", promptNote(m))
	}
	submitSecrets(t, m, secretsNewPass)
	if !strings.Contains(promptNote(m), "Type it again") {
		t.Fatalf("not asked again:\n%s", promptNote(m))
	}
	submitSecrets(t, m, secretsNewPass)
	if m.secretsPromptOpen() {
		t.Fatal("prompt still open")
	}
	if got := toastText(*m); !strings.Contains(got, "Secrets passphrase changed") {
		t.Fatalf("toast %q", got)
	}
	if !ageOpensWith(t, dir, secretsNewPass) || ageOpensWith(t, dir, secretsTestPass) {
		t.Fatal("the file does not open with the new passphrase only")
	}
	if s.Locked() {
		t.Fatal("the store locked")
	}
	if v, _, _ := s.Get("ai.key.openai"); v != secretsTestKey {
		t.Fatalf("value after the change = %q", v)
	}
	if m.secrets.chg != nil {
		t.Fatal("the change state stays")
	}
	if ticks.n > 1 {
		t.Fatalf("%d lock timers", ticks.n)
	}
	for _, where := range []string{toastText(*m), readOrEmpty(filepath.Join(dir, "errors.log"))} {
		if strings.Contains(where, secretsTestPass) || strings.Contains(where, secretsNewPass) {
			t.Fatalf("a passphrase leaked: %q", where)
		}
	}
}

func readOrEmpty(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// Spec (U19 SS-8): a wrong current passphrase is asked again (three
// tries), and the file is never touched.
func TestSecretsChangePassphraseWrongCurrent(t *testing.T) {
	m, _, dir := unlockedAgeStore(t)
	before := readOrEmpty(filepath.Join(dir, secrets.AgeFile))
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-passphrase"))
	for i := 0; i < secretsMaxTries; i++ {
		submitSecrets(t, m, "not the passphrase")
		if i < secretsMaxTries-1 && !strings.Contains(promptNote(m), "Wrong passphrase") {
			t.Fatalf("try %d: %s", i, promptNote(m))
		}
	}
	if m.secretsPromptOpen() || m.secrets.chg != nil {
		t.Fatal("still asking after three wrong tries")
	}
	if got := toastText(*m); !strings.Contains(got, "Passphrase not changed") {
		t.Fatalf("toast %q", got)
	}
	if readOrEmpty(filepath.Join(dir, secrets.AgeFile)) != before {
		t.Fatal("the file changed")
	}
}

// Spec (U19 SS-8): the new passphrase has the floor of 8, a warning
// under 12, must differ from the current one and be typed the same
// twice. Esc forgets everything typed.
func TestSecretsChangePassphraseRules(t *testing.T) {
	m, _, dir := unlockedAgeStore(t)
	before := readOrEmpty(filepath.Join(dir, secrets.AgeFile))
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-passphrase"))
	submitSecrets(t, m, secretsTestPass)
	submitSecrets(t, m, "short")
	if !strings.Contains(promptNote(m), "Too short") {
		t.Fatalf("short: %s", promptNote(m))
	}
	submitSecrets(t, m, secretsTestPass)
	if !strings.Contains(promptNote(m), "current passphrase") || !strings.Contains(promptNote(m), "New passphrase") {
		t.Fatalf("same: %s", promptNote(m))
	}
	submitSecrets(t, m, "elevenchars")
	if !strings.Contains(promptNote(m), "Weak") {
		t.Fatalf("weak: %s", promptNote(m))
	}
	submitSecrets(t, m, "elevenchar2")
	if !strings.Contains(promptNote(m), "differ") || !strings.Contains(promptNote(m), "New passphrase") {
		t.Fatalf("differ: %s", promptNote(m))
	}
	submitSecrets(t, m, secretsNewPass)
	closeSecretsPrompt(t, m)
	if m.secrets.chg != nil {
		t.Fatal("Esc kept the typed passphrases")
	}
	if readOrEmpty(filepath.Join(dir, secrets.AgeFile)) != before {
		t.Fatal("the file changed")
	}
}

// Spec (U19 SS-8): a locked store asks the current passphrase too and
// is open after the change.
func TestSecretsChangePassphraseWhileLocked(t *testing.T) {
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-passphrase"))
	submitSecrets(t, m, secretsTestPass)
	submitSecrets(t, m, secretsNewPass)
	submitSecrets(t, m, secretsNewPass)
	if storeLocked(t) {
		t.Fatal("locked after the change")
	}
	if !ageOpensWith(t, dir, secretsNewPass) {
		t.Fatal("not changed")
	}
}

// Spec (U19 SS-8): the store screen offers Change passphrase… for the
// encrypted file only; the keyring and the plain file have none.
func TestSecretStoreScreenChangePassphraseButton(t *testing.T) {
	m, _, _ := unlockedAgeStore(t)
	m.dispatchPaletteAction("secrets-store")
	if !strings.Contains(storeScreen(m), "Change passphrase…") {
		t.Fatalf("no button:\n%s", storeScreen(m))
	}
	pumpSecrets(t, m, selectStoreRow(t, m, secretActionPassphrase))
	if !m.secretsPromptOpen() || !strings.Contains(promptNote(m), "Current passphrase") {
		t.Fatal("the button did not ask for the current passphrase")
	}
	closeSecretsPrompt(t, m)

	keyring.MockInit()
	for _, backend := range []string{"plain", "keyring"} {
		withSecretSetting(t, backend, true)
		m := secretsTestModel(t)
		m.dispatchPaletteAction("secrets-store")
		if strings.Contains(storeScreen(m), "Change passphrase") {
			t.Fatalf("%s offers a passphrase change:\n%s", backend, storeScreen(m))
		}
		m.pickerOpen = false
		pumpSecrets(t, m, m.dispatchPaletteAction("secrets-passphrase"))
		if m.secretsPromptOpen() || !strings.Contains(toastText(*m), "No passphrase to change") {
			t.Fatalf("%s: toast %q", backend, toastText(*m))
		}
	}
}

// Spec (U19 SS-8): never for the AI.
func TestSecretsChangePassphraseNeverForTheAI(t *testing.T) {
	title := ""
	for _, e := range PaletteCatalog() {
		if e.ID == "secrets-passphrase" {
			title = e.Title
		}
	}
	if title != "AI: Change Secrets Passphrase…" {
		t.Fatalf("palette title %q", title)
	}
	if p, ok := aiActionPolicies["secrets-passphrase"]; !ok || p.Gate != aiGateNever || p.Reason == "" {
		t.Fatalf("policy %+v", p)
	}
}

// closeSecretsPrompt presses Esc in the passphrase prompt.
func closeSecretsPrompt(t *testing.T, m *Model) {
	t.Helper()
	next, _ := m.update(prompt.CloseMsg{})
	*m = next
}
