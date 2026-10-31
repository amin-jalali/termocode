package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/zalando/go-keyring"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/secrets"
	tea "github.com/charmbracelet/bubbletea"
)

// The old plain file with every kind, as a user from before SS-4 has it.
const (
	migKeyOpenAI = "sk-ss4-openai-key-765432"
	migKeyAnth   = "sk-ant-ss4-anthropic-key-1234"
	migClaudeAT  = "at-ss4-claude-access-token"
	migMCPSecret = "ghp_ss4_mcp_secret_value"
	migMCPToken  = "lin-ss4-mcp-oauth-token"
	migDBPass    = "pw-ss4-database-password"
)

var migSecretValues = []string{migKeyOpenAI, migKeyAnth, migClaudeAT, migMCPSecret, migMCPToken, migDBPass}

const migFixture = `{
  "keys": {"openai": "` + migKeyOpenAI + `", "anthropic": "` + migKeyAnth + `"},
  "claude_oauth": {"access_token": "` + migClaudeAT + `", "refresh_token": "rt-ss4", "expires_at_ms": 42},
  "mcp_secrets": {"GITHUB_PAT": "` + migMCPSecret + `"},
  "mcp_oauth": {"linear": {"issuer": "https://linear.app", "access_token": "` + migMCPToken + `"}},
  "mcp_accounts": {"github": "amin"},
  "secrets": {"db.main.password": "` + migDBPass + `"}
}`

// withPlainCredentials gives a test the SS-3 situation: secret_store
// not set and the old ai/credentials.json with every kind. It returns
// the config dir and the plain file's path.
func withPlainCredentials(t *testing.T, probe bool) (dir, cred string) {
	t.Helper()
	dir, _ = withSecretSetting(t, "", probe)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("SSH_CONNECTION", "")
	cred = filepath.Join(dir, "ai", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(cred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte(migFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	old := welcomeSecretsNote
	t.Cleanup(func() { welcomeSecretsNote = old; invalidateWelcome() })
	return dir, cred
}

// startMigrateCheck runs the start-up check like Init does and feeds
// its answer back to Update.
func startMigrateCheck(t *testing.T, m *Model) {
	t.Helper()
	pumpSecrets(t, m, m.secretsMigrateStartCmd())
}

func migrateCardOpen(m *Model) bool {
	return m.confirmOpen && m.confirmKind == confirmKindSecretsMigrate
}

func deletePlainCardOpen(m *Model) bool {
	return m.confirmOpen && m.confirmKind == confirmKindSecretsDeletePlain
}

// deletePlainViaCard checks the safe-first card, then answers Delete.
func deletePlainViaCard(t *testing.T, m *Model, cred string) {
	t.Helper()
	if !deletePlainCardOpen(m) {
		t.Fatal("no delete-plain card")
	}
	if got := buttonIDs(m.confirm); got != "keep,delete" || m.confirm.SafeID() != "keep" {
		t.Fatalf("card is not safe-first: %s", got)
	}
	if b, _ := os.ReadFile(cred); len(b) == 0 {
		t.Fatal("the plain file was deleted before the answer")
	}
	pumpSecrets(t, m, answerStoreConfirm(t, m, "delete"))
	waitGone(t, cred)
}

func migrateCardText(m *Model) string { return ansi.Strip(m.confirm.Box()) }

func buttonIDs(c confirm.Model) string {
	var ids []string
	for _, b := range c.Buttons() {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

func noSecretValues(t *testing.T, where, s string) {
	t.Helper()
	for _, v := range append(migSecretValues, secretsTestPass) {
		if strings.Contains(s, v) {
			t.Fatalf("%s shows a secret value %q:\n%s", where, v, s)
		}
	}
}

// checkMigrated: the plain file is gone, the marker is there, and the
// app's one store holds every name with its value.
func checkMigrated(t *testing.T, dir, cred, backend string) secrets.Store {
	t.Helper()
	if _, err := os.Stat(cred); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credentials.json still there: %v", err)
	}
	if !secrets.Migrated(cred) {
		t.Fatal("no marker")
	}
	s, err := configureSecretStore()
	if err != nil || s.Backend() != backend || s.Locked() {
		t.Fatalf("app store %v err %v", s, err)
	}
	for n, w := range map[string]string{"ai.key.openai": migKeyOpenAI, "ai.key.anthropic": migKeyAnth,
		"mcp.secret.GITHUB_PAT": migMCPSecret, "db.main.password": migDBPass, "mcp.account.github": "amin"} {
		if v, ok, err := s.Get(n); v != w || !ok || err != nil {
			t.Errorf("%s = %q %v %v", n, v, ok, err)
		}
	}
	c, err := ai.LoadCredentials(cred)
	if err != nil || c.ClaudeOAuth == nil || c.ClaudeOAuth.AccessToken != migClaudeAT || c.MCPOAuth["linear"].AccessToken != migMCPToken {
		t.Fatalf("credentials view after the move: %v", err)
	}
	if e := secrets.Entries(dir); e["ai.key.openai"].Kind != secrets.KindValue {
		t.Fatalf("index %+v", e)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, secrets.IndexFile)); strings.Contains(string(b), migKeyOpenAI) {
		t.Fatal("a value reached secrets.json")
	}
	return s
}

func TestSecretsMigrateCardShowsCountsNeverValues(t *testing.T) {
	withPlainCredentials(t, false)
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	if !migrateCardOpen(m) {
		t.Fatal("no migration card")
	}
	text := migrateCardText(m)
	// The reason depends on the OS: no D-Bus on Linux, else the fake
	// probe's "did not answer".
	why := secrets.KeyringSkipReason(nil, false)
	if why == "" {
		why = "it did not answer"
	}
	for _, want := range []string{"Move your secrets to a safer store?", "ai/credentials.json",
		"2 AI keys · Claude sign-in · 1 MCP secret · 1 sign-in · 1 other secret",
		"encrypted file", why, "Encrypted file", "Keep plain"} {
		if !strings.Contains(strings.Join(strings.Fields(text), " "), want) {
			t.Errorf("card misses %q:\n%s", want, text)
		}
	}
	noSecretValues(t, "the card", text)
	if got := buttonIDs(m.confirm); got != "age,plain" {
		t.Errorf("buttons %s", got)
	}

	// A keyring that answers is the recommendation and the first button.
	withPlainCredentials(t, true)
	m = secretsTestModel(t)
	startMigrateCheck(t, m)
	if !migrateCardOpen(m) {
		t.Fatal("no card with a keyring")
	}
	if got := buttonIDs(m.confirm); got != "keyring,age,plain" {
		t.Errorf("buttons %s", got)
	}
	if text := migrateCardText(m); !strings.Contains(text, "OS keyring") || strings.Contains(text, "not available") {
		t.Errorf("keyring card:\n%s", text)
	}
}

func TestSecretsMigrateToAge(t *testing.T) {
	dir, cred := withPlainCredentials(t, false)
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	cmd := answerStoreConfirm(t, m, "age")
	pumpSecrets(t, m, cmd)
	if !m.secretsPromptOpen() || !m.prompt.Masked() || !m.secrets.creating {
		t.Fatalf("no new-passphrase prompt (open=%v)", m.secretsPromptOpen())
	}
	submitSecrets(t, m, secretsTestPass)
	if !strings.Contains(m.prompt.Note(), "again") {
		t.Fatalf("not asked twice: %q", m.prompt.Note())
	}
	if b, _ := os.ReadFile(cred); string(b) != migFixture {
		t.Fatal("the file changed before the second entry")
	}
	submitSecrets(t, m, secretsTestPass)
	if m.secretsPromptOpen() {
		t.Fatal("prompt still open")
	}
	if got := toastText(*m); !strings.Contains(got, "Secrets moved · plain file kept") {
		t.Fatalf("toast %q", got)
	}
	noSecretValues(t, "the toast", toastText(*m))
	deletePlainViaCard(t, m, cred)
	checkMigrated(t, dir, cred, "age")
	if !m.aiConfigured() {
		t.Fatal("the AI lost its key after the move")
	}
	// Next start: the encrypted file, locked; the plain rule is gone and
	// the card does not come back.
	resetSecretStore()
	if s, _ := configureSecretStore(); s.Backend() != "age" || !s.Locked() {
		t.Fatalf("restart: %s locked=%v", s.Backend(), s.Locked())
	}
	m2 := secretsTestModel(t)
	startMigrateCheck(t, m2)
	if migrateCardOpen(m2) {
		t.Fatal("card after the move")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "errors.log")); strings.Contains(string(b), secretsTestPass) || strings.Contains(string(b), migKeyOpenAI) {
		t.Fatalf("errors.log holds a secret: %s", b)
	}
}

func TestSecretsMigrateToKeyring(t *testing.T) {
	keyring.MockInit()
	dir, cred := withPlainCredentials(t, true)
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	pumpSecrets(t, m, answerStoreConfirm(t, m, "keyring"))
	if m.secretsPromptOpen() {
		t.Fatal("the keyring asked for a passphrase")
	}
	if got := toastText(*m); !strings.Contains(got, "Secrets moved · plain file kept") {
		t.Fatalf("toast %q", got)
	}
	deletePlainViaCard(t, m, cred)
	checkMigrated(t, dir, cred, "keyring")
	resetSecretStore()
	if s, _ := configureSecretStore(); s.Backend() != "keyring" {
		t.Fatalf("restart: %s", s.Backend())
	}
}

// A failed move (here: an existing encrypted file and a wrong
// passphrase) keeps the plain file and the plain store.
func TestSecretsMigrateFailureKeepsPlainFile(t *testing.T) {
	dir, cred := withPlainCredentials(t, false)
	writeAgeStore(t, dir) // made under another passphrase
	if err := os.WriteFile(filepath.Join(dir, secrets.IndexFile), []byte(`{"version":1,"entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resetSecretStore()
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	pumpSecrets(t, m, answerStoreConfirm(t, m, "age"))
	if !m.secretsPromptOpen() || m.secrets.creating {
		t.Fatal("an existing file must ask for its passphrase once")
	}
	for i := 0; i < secretsMaxTries; i++ {
		submitSecrets(t, m, "not the passphrase")
	}
	if m.secretsPromptOpen() || !strings.Contains(toastText(*m), "Secrets not moved") {
		t.Fatalf("after three wrong tries: open=%v toast %q", m.secretsPromptOpen(), toastText(*m))
	}
	if b, _ := os.ReadFile(cred); string(b) != migFixture || secrets.Migrated(cred) {
		t.Fatal("the plain file was touched")
	}
	if s, _ := configureSecretStore(); s.Backend() != "plain" {
		t.Fatalf("store %s", s.Backend())
	}
	resetSecretStore()
	if s, _ := configureSecretStore(); s.Backend() != "plain" {
		t.Fatalf("restart: %s", s.Backend())
	}
}

func TestSecretsMigrateKeepPlain(t *testing.T) {
	_, cred := withPlainCredentials(t, false)
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	pumpSecrets(t, m, answerStoreConfirm(t, m, "plain"))
	if got := loadSettings().SecretStore; got != "plain" {
		t.Fatalf("secret_store = %q", got)
	}
	if b, _ := os.ReadFile(cred); string(b) != migFixture {
		t.Fatal("plain file changed")
	}
	resetSecretStore()
	m2 := secretsTestModel(t)
	startMigrateCheck(t, m2)
	if migrateCardOpen(m2) {
		t.Fatal("the card came back after Keep plain")
	}
	// The palette entry still offers the move.
	pumpSecrets(t, m2, m2.dispatchPaletteAction("secrets-migrate"))
	if !migrateCardOpen(m2) {
		t.Fatal("palette did not open the card")
	}
	// Moving later clears the plain setting: auto finds the new store.
	pumpSecrets(t, m2, answerStoreConfirm(t, m2, "age"))
	submitSecrets(t, m2, secretsTestPass)
	submitSecrets(t, m2, secretsTestPass)
	if got := loadSettings().SecretStore; got != "" {
		t.Fatalf("secret_store after the move = %q", got)
	}
	if s, _ := configureSecretStore(); s.Backend() != "age" || s.Locked() {
		t.Fatalf("store %s locked=%v", s.Backend(), s.Locked())
	}
	if _, err := os.Stat(cred); err != nil {
		t.Fatalf("the plain file is gone without an answer: %v", err)
	}
	deletePlainViaCard(t, m2, cred)
}

// Keep for now leaves the plain file; Esc and Enter both keep it.
func TestSecretsDeletePlainKeepsByDefault(t *testing.T) {
	keyring.MockInit()
	_, cred := withPlainCredentials(t, true)
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	pumpSecrets(t, m, answerStoreConfirm(t, m, "keyring"))
	if !deletePlainCardOpen(m) {
		t.Fatal("no delete-plain card after the move")
	}
	if _, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("Enter did nothing")
	} else if msg, ok := cmd().(confirm.SelectMsg); !ok || msg.ID != "keep" {
		t.Fatalf("Enter = %#v", msg)
	}
	next, cmd := m.update(confirm.CloseMsg{}) // Esc
	*m = next
	pumpSecrets(t, m, cmd)
	if _, err := os.Stat(cred); err != nil {
		t.Fatalf("Esc deleted the plain file: %v", err)
	}
	if got := toastText(*m); !strings.Contains(got, "Plain file kept") {
		t.Fatalf("toast %q", got)
	}
	// The palette entry asks again.
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-migrate"))
	if !deletePlainCardOpen(m) {
		t.Fatal("palette did not offer the delete again")
	}
}

func TestSecretsMigrateLaterThreeTimesThenNote(t *testing.T) {
	dir, _ := withPlainCredentials(t, false)
	for i := 1; i <= secretsMigrateMaxLater; i++ {
		m := secretsTestModel(t)
		startMigrateCheck(t, m)
		if !migrateCardOpen(m) {
			t.Fatalf("start %d: no card", i)
		}
		next, cmd := m.update(confirm.CloseMsg{})
		*m = next
		pumpSecrets(t, m, cmd)
		if secrets.LaterCount(dir) != i {
			t.Fatalf("start %d: count %d", i, secrets.LaterCount(dir))
		}
	}
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	if migrateCardOpen(m) {
		t.Fatal("asked a fourth time")
	}
	if !welcomeSecretsNote {
		t.Fatal("no Welcome note")
	}
	invalidateWelcome()
	if page := ansi.Strip(renderWelcome(100, 40, -1, true)); !strings.Contains(page, "F1 → AI: Move Secrets to a Safer Store…") {
		t.Fatalf("Welcome page without the note:\n%s", page)
	}
	// The palette entry still opens the card.
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-migrate"))
	if !migrateCardOpen(m) {
		t.Fatal("palette did not open the card")
	}
}

// A move cut off after the copy: the store holds the same names; the
// next start asks (Keep for now first) before it removes the plain file.
func TestSecretsMigrateFinishesInterruptedMove(t *testing.T) {
	keyring.MockInit()
	dir, cred := withPlainCredentials(t, true)
	p, err := secrets.FromPlain(cred)
	if err != nil {
		t.Fatal(err)
	}
	s, err := secrets.Open(secrets.Options{Dir: dir, PlainPath: cred, Backend: "keyring", KeyringProbe: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.SetAll(s, p.Values); err != nil {
		t.Fatal(err)
	}
	resetSecretStore()
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	if migrateCardOpen(m) || m.secretsPromptOpen() {
		t.Fatal("asked to move during the finish")
	}
	deletePlainViaCard(t, m, cred)
	checkMigrated(t, dir, cred, "keyring")
}

// With the encrypted file the finish waits for the user's own unlock.
func TestSecretsMigrateFinishesAfterUnlock(t *testing.T) {
	dir, cred := withPlainCredentials(t, false)
	p, _ := secrets.FromPlain(cred)
	s, err := secrets.Open(secrets.Options{Dir: dir, PlainPath: cred, Backend: "age", ScryptWorkFactor: 14})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	if err := secrets.SetAll(s, p.Values); err != nil {
		t.Fatal(err)
	}
	s.Lock()
	resetSecretStore()
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	if migrateCardOpen(m) || deletePlainCardOpen(m) {
		t.Fatal("card while the store is locked")
	}
	if b, _ := os.ReadFile(cred); string(b) != migFixture {
		t.Fatal("removed before the store could be checked")
	}
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-unlock"))
	submitSecrets(t, m, secretsTestPass)
	deletePlainViaCard(t, m, cred)
	checkMigrated(t, dir, cred, "age")
}

// waitGone waits up to 30 s for path to disappear and for the move's
// marker after it (the finish deletes the file, then writes the marker).
func waitGone(t *testing.T, path string) {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) && secrets.Migrated(path) {
			return
		}
	}
}

func TestSecretsMigrateNeverAsksWhenDoneOrEmpty(t *testing.T) {
	// Already moved: the marker and no plain file.
	dir, cred := withPlainCredentials(t, false)
	if err := os.Remove(cred); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secrets.MarkerPath(cred), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	if migrateCardOpen(m) {
		t.Fatal("asked after the move")
	}
	// The marker also stops the card when a plain file came back.
	if err := os.WriteFile(cred, []byte(migFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	resetSecretStore()
	m = secretsTestModel(t)
	startMigrateCheck(t, m)
	if migrateCardOpen(m) {
		t.Fatal("asked although the marker is there")
	}
	// A fresh setup (no plain file) and an empty one never ask.
	for _, content := range []string{"", "{}"} {
		_ = os.Remove(secrets.MarkerPath(cred))
		_ = os.Remove(cred)
		if content != "" {
			_ = os.WriteFile(cred, []byte(content), 0o600)
		}
		_ = os.Remove(filepath.Join(dir, secrets.IndexFile))
		resetSecretStore()
		m = secretsTestModel(t)
		startMigrateCheck(t, m)
		if migrateCardOpen(m) || welcomeSecretsNote {
			t.Fatalf("asked with %q", content)
		}
	}
}

// TestMain keeps the plain setting for this package: no card there.
func TestSecretsMigrateNotUnderPlainSetting(t *testing.T) {
	withPlainCredentials(t, false)
	cfg, _ := json.Marshal(map[string]any{"secret_store": "plain"})
	d, _ := ai.ConfigDir()
	if err := os.WriteFile(filepath.Join(d, "config.json"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	resetSecretStore()
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	if migrateCardOpen(m) {
		t.Fatal("asked although secret_store is plain")
	}
}

func TestSecretsMigratePaletteNeverForTheAI(t *testing.T) {
	title := ""
	for _, e := range PaletteCatalog() {
		if e.ID == "secrets-migrate" {
			title = e.Title
		}
	}
	if title != "AI: Move Secrets to a Safer Store…" {
		t.Fatalf("palette title %q", title)
	}
	if p, ok := aiActionPolicies["secrets-migrate"]; !ok || p.Gate != aiGateNever || p.Reason == "" {
		t.Fatalf("policy %+v", p)
	}
	// Not plain: nothing to move, a toast says where the secrets are.
	withSecretSetting(t, "age", false)
	m := secretsTestModel(t)
	pumpSecrets(t, m, m.dispatchPaletteAction("secrets-migrate"))
	if migrateCardOpen(m) || !strings.Contains(toastText(*m), "encrypted file") {
		t.Fatalf("card=%v toast %q", migrateCardOpen(m), toastText(*m))
	}
}

// Spec (FIX-24): when the move ends while another card is on screen (the
// Restore unsaved changes? card of the start), the Delete the old plain
// file? card does not replace it; it waits and opens once that card is
// answered.
func TestSecretsDeletePlainCardWaitsForOtherCard(t *testing.T) {
	keyring.MockInit()
	_, cred := withPlainCredentials(t, true)
	m := secretsTestModel(t)
	startMigrateCheck(t, m)
	cmd := answerStoreConfirm(t, m, "keyring")
	m.confirm = confirm.New("Other card", "x", []confirm.Button{{ID: "ok", Title: "OK"}})
	m.confirmOpen, m.confirmKind = true, confirmKindHotExit
	pumpSecrets(t, m, cmd)
	if !m.confirmOpen || m.confirmKind != confirmKindHotExit {
		t.Fatalf("the other card was replaced (kind %v)", m.confirmKind)
	}
	m.confirmOpen = false // the other card is answered
	next, _ := m.update(struct{}{})
	*m = next
	if !deletePlainCardOpen(m) {
		t.Fatal("the delete-plain card did not open after the other card")
	}
	if _, err := os.Stat(cred); err != nil {
		t.Fatalf("the plain file went before the answer: %v", err)
	}
}
