package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/amin-jalali/termocode/internal/secrets"
)

// brokenAgeStore makes a damaged secrets.age and an index that lists a
// value in it.
func brokenAgeStore(t *testing.T) (dir, path string) {
	t.Helper()
	dir, _ = withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	path = filepath.Join(dir, secrets.AgeFile)
	if err := os.WriteFile(path, []byte("this is not an age file"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

// Spec (U19 SS-8, plan §5): unlocking a damaged secrets.age names the
// file and offers a new one; the old file is kept as
// secrets.age.broken-<date>, never deleted.
func TestSecretsCorruptFileOffersNewFile(t *testing.T) {
	advance := fakeNow(t)
	advance(0)
	dir, path := brokenAgeStore(t)
	m := secretsTestModel(t)
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-unlock"))
	submitSecrets(t, m, secretsTestPass)
	if !m.confirmOpen || m.confirmKind != confirmKindSecretsBroken {
		t.Fatalf("no card for the damaged file (toast %q)", toastText(*m))
	}
	text := migrateCardText(m)
	if !strings.Contains(strings.Join(strings.Fields(text), ""), strings.Join(strings.Fields(path), "")) ||
		!strings.Contains(text, "Start a new encrypted file") {
		t.Fatalf("card:\n%s", text)
	}
	pumpSecrets(t, m, answerStoreConfirm(t, m, "new"))
	kept := path + ".broken-20261022-090000"
	if b, err := os.ReadFile(kept); err != nil || string(b) != "this is not an age file" {
		t.Fatalf("kept file: %q %v", b, err)
	}
	if !m.secretsPromptOpen() || !strings.Contains(promptNote(m), "New secrets passphrase") {
		t.Fatalf("no new passphrase prompt:\n%s", promptNote(m))
	}
	submitSecrets(t, m, secretsNewPass)
	submitSecrets(t, m, secretsNewPass)
	if storeLocked(t) {
		t.Fatal("the new file is not open")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no new secrets.age: %v", err)
	}
	if _, ok := secrets.Entries(dir)["ai.key.openai"]; ok {
		t.Error("the lost value is still listed")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatal("the kept file went away")
	}
}

// Spec (U19 SS-8): Cancel on that card keeps the damaged file as it is.
func TestSecretsCorruptFileCancelKeepsIt(t *testing.T) {
	_, path := brokenAgeStore(t)
	m := secretsTestModel(t)
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-unlock"))
	submitSecrets(t, m, secretsTestPass)
	answerStoreConfirm(t, m, "cancel")
	if b, err := os.ReadFile(path); err != nil || string(b) != "this is not an age file" {
		t.Fatalf("file: %q %v", b, err)
	}
	matches, _ := filepath.Glob(path + ".broken-*")
	if len(matches) != 0 {
		t.Fatalf("kept files after Cancel: %v", matches)
	}
}

// Spec (U19 SS-8, plan §5): a keyring that does not answer any more
// shows one toast per process, whatever the number of failed reads.
func TestSecretsKeyringUnreachableToastOnce(t *testing.T) {
	resetKeyringNotice()
	t.Cleanup(resetKeyringNotice)
	keyring.MockInit()
	withSecretSetting(t, "keyring", true)
	s, err := configureSecretStore()
	if err != nil || s.Backend() != "keyring" {
		t.Fatalf("store %v %v", s, err)
	}
	keyring.MockInitWithError(errors.New("dbus: no session"))
	t.Cleanup(keyring.MockInit)
	m := secretsTestModel(t)
	shown := 0
	for i := 0; i < 3; i++ {
		_, _, _ = s.Get("ai.key.openai")
		next, _ := m.Update(struct{}{})
		*m = next.(Model)
		shown = strings.Count(toastText(*m), "Keyring not reachable here")
	}
	if shown != 1 || !strings.Contains(toastText(*m), "F1 → AI: Secret Store…") {
		t.Fatalf("toasts: %q", toastText(*m))
	}
}

// The notice costs nothing while the keyring works: no toast, no
// command.
func TestSecretsKeyringNoticeQuietWhenFine(t *testing.T) {
	resetKeyringNotice()
	t.Cleanup(resetKeyringNotice)
	m := secretsTestModel(t)
	if cmd := m.drainKeyringNotice(); cmd != nil {
		t.Fatal("a notice with no failure")
	}
}
