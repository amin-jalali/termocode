package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// switchCard opens the store screen and presses Switch backend….
func switchCard(t *testing.T, m *Model) string {
	t.Helper()
	m.dispatchPaletteAction("secrets-store")
	if !strings.Contains(storeScreen(m), "Switch backend…") {
		t.Fatalf("no Switch backend… button:\n%s", storeScreen(m))
	}
	pumpSecrets(t, m, selectStoreRow(t, m, secretActionSwitch))
	if !m.confirmOpen || m.confirmKind != confirmKindSecretsSwitch {
		t.Fatalf("no switch card (confirm %v kind %v)", m.confirmOpen, m.confirmKind)
	}
	return migrateCardText(m)
}

func checkStoreValues(t *testing.T, s secrets.Store) {
	t.Helper()
	for n, want := range map[string]string{"ai.key.openai": secretsTestKey, "db.main.password": "pw-ss3b-very-secret"} {
		if v, ok, err := s.Get(n); v != want || !ok || err != nil {
			t.Fatalf("Get(%s) = %q %v %v", n, v, ok, err)
		}
	}
}

// Spec (U19 SS-8): Switch backend… copies everything to the OS keyring,
// records it (index and secret_store), removes the encrypted file and
// keeps working after a restart.
func TestSecretsSwitchAgeToKeyring(t *testing.T) {
	keyring.MockInit()
	m, _, dir := unlockedAgeStore(t)
	secretKeyringProbe = func() bool { return true }
	text := switchCard(t, m)
	if ids := buttonIDs(m.confirm); !strings.Contains(ids, "keyring") || !strings.Contains(ids, "plain") || strings.Contains(ids, "age") {
		t.Fatalf("buttons %s\n%s", ids, text)
	}
	pumpSecrets(t, m, answerStoreConfirm(t, m, "keyring"))
	if got := toastText(*m); !strings.Contains(got, "Secrets now in the OS keyring") {
		t.Fatalf("toast %q", got)
	}
	s, err := configureSecretStore()
	if err != nil || s.Backend() != "keyring" {
		t.Fatalf("store %v %v", s, err)
	}
	checkStoreValues(t, s)
	if got := loadSettings().SecretStore; got != "keyring" {
		t.Fatalf("secret_store = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, secrets.AgeFile)); !os.IsNotExist(err) {
		t.Fatalf("secrets.age still there: %v", err)
	}
	if e := secrets.Entries(dir)["mcp.secret.GITHUB_PAT"]; e.Kind != secrets.KindRef {
		t.Fatalf("the reference became %+v", e)
	}
	resetSecretStore()
	again, _ := configureSecretStore()
	if again.Backend() != "keyring" {
		t.Fatalf("restart: %s", again.Backend())
	}
	checkStoreValues(t, again)
	noSecretValues(t, "toast", toastText(*m))
}

// Spec (U19 SS-8): without a working keyring the card says why and
// does not offer it.
func TestSecretsSwitchKeyringNotAvailableSaysWhy(t *testing.T) {
	m, _, _ := unlockedAgeStore(t)
	secretKeyringProbe = func() bool { return false }
	text := switchCard(t, m)
	if strings.Contains(buttonIDs(m.confirm), "keyring") || !strings.Contains(text, "OS keyring: not available here") {
		t.Fatalf("buttons %s\n%s", buttonIDs(m.confirm), text)
	}
}

// Spec (U19 SS-8): to the encrypted file asks a new passphrase twice;
// the plain file is kept until the user deletes it on a card.
func TestSecretsSwitchPlainToAge(t *testing.T) {
	ticks := fakeLockTicks(t)
	dir, _ := withSecretSetting(t, "plain", false)
	setLockMinutes(t, dir, 5)
	s, _ := configureSecretStore()
	if err := s.Set("ai.key.openai", secretsTestKey); err != nil {
		t.Fatal(err)
	}
	cred, _ := ai.CredentialsPath()
	m := secretsTestModel(t)
	switchCard(t, m)
	pumpSecrets(t, m, answerStoreConfirm(t, m, "age"))
	if !m.secretsPromptOpen() || !strings.Contains(promptNote(m), "New secrets passphrase") {
		t.Fatalf("no new passphrase prompt:\n%s", promptNote(m))
	}
	submitSecrets(t, m, secretsTestPass)
	submitSecrets(t, m, secretsTestPass)
	if got := toastText(*m); !strings.Contains(got, "Secrets now in the encrypted file") {
		t.Fatalf("toast %q", got)
	}
	s, _ = configureSecretStore()
	if s.Backend() != "age" || s.Locked() {
		t.Fatalf("store %s locked=%v", s.Backend(), s.Locked())
	}
	if v, _, _ := s.Get("ai.key.openai"); v != secretsTestKey {
		t.Fatalf("Get = %q", v)
	}
	deletePlainViaCard(t, m, cred)
	if _, err := os.Stat(cred); !os.IsNotExist(err) {
		t.Fatalf("credentials.json still there: %v", err)
	}
	if ticks.n != 1 {
		t.Fatalf("%d lock timers after the switch to the encrypted file", ticks.n)
	}
}

// Spec (U19 SS-8): the plain file needs a second, clear yes; Cancel
// changes nothing.
func TestSecretsSwitchToPlainWarns(t *testing.T) {
	m, _, dir := unlockedAgeStore(t)
	switchCard(t, m)
	answerStoreConfirm(t, m, "plain")
	if !m.confirmOpen || m.confirmKind != confirmKindSecretsSwitchPlain || !strings.Contains(migrateCardText(m), "not encrypted") {
		t.Fatalf("no warning card:\n%s", migrateCardText(m))
	}
	answerStoreConfirm(t, m, "cancel")
	if s, _ := configureSecretStore(); s.Backend() != "age" {
		t.Fatalf("Cancel switched to %s", s.Backend())
	}
	if _, err := os.Stat(filepath.Join(dir, secrets.AgeFile)); err != nil {
		t.Fatal(err)
	}
	m.confirmOpen, m.pickerOpen = false, false
	switchCard(t, m)
	answerStoreConfirm(t, m, "plain")
	pumpSecrets(t, m, answerStoreConfirm(t, m, "use-plain"))
	s, _ := configureSecretStore()
	if s.Backend() != "plain" {
		t.Fatalf("backend %s", s.Backend())
	}
	checkStoreValues(t, s)
	if got := loadSettings().SecretStore; got != "plain" {
		t.Fatalf("secret_store = %q", got)
	}
}

// Spec (U19 SS-8): when the switch fails (here: config.json can not be
// written) the old store stays in use and nothing is deleted.
func TestSecretsSwitchFailureKeepsOldStore(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes read-only files")
	}
	m, _, dir := unlockedAgeStore(t)
	cfg := filepath.Join(dir, "config.json")
	if err := os.Chmod(cfg, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfg, 0o644) })
	switchCard(t, m)
	answerStoreConfirm(t, m, "plain")
	pumpSecrets(t, m, answerStoreConfirm(t, m, "use-plain"))
	if got := toastText(*m); !strings.Contains(got, "Backend not switched") {
		t.Fatalf("toast %q", got)
	}
	s, _ := configureSecretStore()
	if s.Backend() != "age" || s.Locked() {
		t.Fatalf("store %s locked=%v", s.Backend(), s.Locked())
	}
	checkStoreValues(t, s)
	cred, _ := ai.CredentialsPath()
	if _, err := os.Stat(cred); !os.IsNotExist(err) {
		t.Fatalf("a plain copy was left: %v", err)
	}
}

// Spec (U19 SS-8): a locked store asks for its passphrase before the
// switch card.
func TestSecretsSwitchLockedStoreUnlocksFirst(t *testing.T) {
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	m.dispatchPaletteAction("secrets-store")
	pumpSecrets(t, m, selectStoreRow(t, m, secretActionSwitch))
	if !m.secretsPromptOpen() {
		t.Fatal("no passphrase prompt on a locked store")
	}
	submitSecrets(t, m, secretsTestPass)
	if !m.confirmOpen || m.confirmKind != confirmKindSecretsSwitch {
		t.Fatal("no switch card after the unlock")
	}
}
