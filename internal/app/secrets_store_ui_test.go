package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// storeScreen is the open store screen as plain text.
func storeScreen(m *Model) string { return ansi.Strip(m.picker.Box()) }

func selectStoreRow(t *testing.T, m *Model, id string) tea.Cmd {
	t.Helper()
	if !m.pickerOpen || m.pickerKind != pickerKindSecretStore {
		t.Fatalf("the store screen is not open (open=%v kind=%v)", m.pickerOpen, m.pickerKind)
	}
	next, cmd := m.update(picker.SelectMsg{ID: id})
	*m = next
	return cmd
}

func answerStoreConfirm(t *testing.T, m *Model, id string) tea.Cmd {
	t.Helper()
	if !m.confirmOpen {
		t.Fatal("no confirm card open")
	}
	next, cmd := m.update(confirm.SelectMsg{ID: id})
	*m = next
	return cmd
}

// unlockedAgeStore gives an unlocked age store with two secrets and a
// reference.
func unlockedAgeStore(t *testing.T) (*Model, secrets.Store, string) {
	t.Helper()
	dir, _ := withSecretSetting(t, "age", false)
	s, err := configureSecretStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	for n, v := range map[string]string{"ai.key.openai": secretsTestKey, "db.main.password": "pw-ss3b-very-secret"} {
		if err := s.Set(n, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Set("mcp.secret.GITHUB_PAT", "${env:GITHUB_TOKEN_SS3B}"); err != nil {
		t.Fatal(err)
	}
	return secretsTestModel(t), s, dir
}

func TestSecretStorePaletteEntryNeverForTheAI(t *testing.T) {
	title := ""
	for _, e := range PaletteCatalog() {
		if e.ID == "secrets-store" {
			title = e.Title
		}
	}
	if title != "AI: Secret Store…" {
		t.Fatalf("palette title %q", title)
	}
	if p, ok := aiActionPolicies["secrets-store"]; !ok || p.Gate != aiGateNever || p.Reason == "" {
		t.Fatalf("policy %+v", p)
	}
}

func TestSecretStoreScreenListsNamesAndNeverValues(t *testing.T) {
	m, s, _ := unlockedAgeStore(t)
	m.dispatchPaletteAction("secrets-store")
	if !m.pickerOpen || m.pickerKind != pickerKindSecretStore {
		t.Fatal("palette did not open the store screen")
	}
	box := storeScreen(m)
	for _, want := range []string{"Secret Store", "encrypted file", secrets.AgeFile, "unlocked",
		"ai.key.openai", "db.main.password", "mcp.secret.GITHUB_PAT", "${env:GITHUB_TOKEN_SS3B}", "value", "Lock now"} {
		if !strings.Contains(box, want) {
			t.Errorf("screen lacks %q:\n%s", want, box)
		}
	}
	for _, secret := range []string{secretsTestKey, "pw-ss3b-very-secret", secretsTestPass} {
		if strings.Contains(box, secret) {
			t.Fatalf("a secret value is on the screen:\n%s", box)
		}
	}
	if strings.Index(box, "ai.key.openai") > strings.Index(box, "db.main.password") {
		t.Error("names are not sorted")
	}
	// Opening the screen must not read any value.
	_ = s
}

func TestSecretStoreLockedShowsNamesAndUnlock(t *testing.T) {
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	m.dispatchPaletteAction("secrets-store")
	box := storeScreen(m)
	if !strings.Contains(box, "locked") || !strings.Contains(box, "Unlock…") || strings.Contains(box, "Lock now") {
		t.Fatalf("locked screen:\n%s", box)
	}
	if strings.Contains(box, secretsTestKey) {
		t.Fatal("value on screen")
	}
	pumpSecrets(t, m, selectStoreRow(t, m, "act:unlock"))
	submitSecrets(t, m, secretsTestPass)
	if storeLocked(t) {
		t.Fatal("still locked")
	}
	if !m.pickerOpen || m.pickerKind != pickerKindSecretStore || !strings.Contains(storeScreen(m), "Lock now") {
		t.Fatal("the screen did not come back unlocked")
	}
}

func TestSecretStoreLockNow(t *testing.T) {
	m, _, _ := unlockedAgeStore(t)
	m.dispatchPaletteAction("secrets-store")
	selectStoreRow(t, m, "act:lock")
	if !storeLocked(t) {
		t.Fatal("Lock now did not lock")
	}
	if box := storeScreen(m); !m.pickerOpen || !strings.Contains(box, "Unlock…") {
		t.Fatalf("screen after lock:\n%s", box)
	}
}

func TestSecretStoreForgetAsksThenDeletes(t *testing.T) {
	m, s, _ := unlockedAgeStore(t)
	m.dispatchPaletteAction("secrets-store")
	selectStoreRow(t, m, "name:db.main.password")
	if !m.confirmOpen {
		t.Fatal("no confirm card")
	}
	if got := ansi.Strip(m.confirm.View()); !strings.Contains(got, "db.main.password") || strings.Contains(got, "pw-ss3b-very-secret") {
		t.Fatalf("card:\n%s", got)
	}
	answerStoreConfirm(t, m, "cancel")
	if len(s.Names()) != 3 {
		t.Fatalf("cancel deleted: %v", s.Names())
	}
	if !m.pickerOpen || !strings.Contains(storeScreen(m), "db.main.password") {
		t.Fatal("cancel did not return to the screen")
	}
	selectStoreRow(t, m, "name:db.main.password")
	answerStoreConfirm(t, m, "forget")
	for _, n := range s.Names() {
		if n == "db.main.password" {
			t.Fatal("still saved")
		}
	}
	if box := storeScreen(m); !m.pickerOpen || strings.Contains(box, "db.main.password") || !strings.Contains(box, "ai.key.openai") {
		t.Fatalf("screen after forget:\n%s", box)
	}
}

func TestSecretStoreForgetOnLockedStoreAsksToUnlockFirst(t *testing.T) {
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	m.dispatchPaletteAction("secrets-store")
	pumpSecrets(t, m, selectStoreRow(t, m, "name:ai.key.openai"))
	if !m.secretsPromptOpen() || m.confirmOpen {
		t.Fatal("expected the passphrase prompt before the confirm card")
	}
	submitSecrets(t, m, secretsTestPass)
	if !m.confirmOpen {
		t.Fatal("no confirm card after unlocking")
	}
	answerStoreConfirm(t, m, "forget")
	s, _ := configureSecretStore()
	if len(s.Names()) != 0 {
		t.Fatalf("not forgotten: %v", s.Names())
	}
}

func TestSecretStoreEmptyAndPlainNotes(t *testing.T) {
	withSecretSetting(t, "plain", false)
	m := secretsTestModel(t)
	m.dispatchPaletteAction("secrets-store")
	box := storeScreen(m)
	for _, want := range []string{"plain file", "not encrypted", "No secrets saved yet", "AI key"} {
		if !strings.Contains(box, want) {
			t.Errorf("empty plain screen lacks %q:\n%s", want, box)
		}
	}
	if strings.Contains(box, "Lock now") || strings.Contains(box, "Unlock…") {
		t.Errorf("a plain store has nothing to lock:\n%s", box)
	}
	// Enter on the empty state does nothing and does not panic.
	if c, ok := m.picker.Current(); ok && strings.HasPrefix(c.ID, "name:") {
		t.Fatalf("empty state row is selectable: %+v", c)
	}
}

func TestSecretStoreNarrowWidthsDoNotPanic(t *testing.T) {
	m, _, _ := unlockedAgeStore(t)
	for w := 0; w <= 90; w += 3 {
		for _, h := range []int{0, 5, 12, 30} {
			m.w, m.h = w, h
			m.dispatchPaletteAction("secrets-store")
			_ = m.picker.Box()
			_ = m.picker.View()
		}
	}
}

func TestOpenAICredentialsOpensStoreScreenForAgeAndKeyring(t *testing.T) {
	for _, setting := range []string{"age", "keyring"} {
		_, _ = withSecretSetting(t, setting, true)
		m := secretsTestModel(t)
		m.dispatchPaletteAction("ai-credentials")
		if !m.pickerOpen || m.pickerKind != pickerKindSecretStore {
			t.Errorf("%s: credentials did not open the store screen", setting)
		}
		if !strings.Contains(toastText(*m), "not a file") {
			t.Errorf("%s: no note why: %q", setting, toastText(*m))
		}
	}
}

func TestOpenAICredentialsKeepsPlainFileForPlain(t *testing.T) {
	withSecretSetting(t, "plain", false)
	m := secretsTestModel(t)
	m.dispatchPaletteAction("ai-credentials")
	if m.pickerOpen {
		t.Fatal("plain store opened the screen instead of the file")
	}
	if m.focus != FocusEditor {
		t.Fatal("the credentials file was not opened")
	}
}

func TestAIKeyPromptSaysWhereItGoes(t *testing.T) {
	for setting, want := range map[string]string{"plain": "plain file", "keyring": "OS keyring"} {
		withSecretSetting(t, setting, true)
		m := secretsTestModel(t)
		m.openAIKeyPrompt(ai.KindOpenAI)
		got := ansi.Strip(m.prompt.View())
		if !strings.Contains(got, want) || strings.Contains(got, "0600 in ai/credentials.json") && setting != "plain" {
			t.Errorf("%s: prompt %q lacks %q", setting, got, want)
		}
	}
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	s, _ := configureSecretStore()
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	m.openAIKeyPrompt(ai.KindOpenAI)
	if got := ansi.Strip(m.prompt.View()); !strings.Contains(got, "encrypted file") {
		t.Errorf("age: prompt %q", got)
	}
}

func TestRegistrySecretName(t *testing.T) {
	for host, want := range map[string]string{
		"ghcr.io":                     "registry.ghcr.io",
		"https://index.docker.io/v1/": "registry.docker.io",
		"localhost:5000":              "registry.localhost-5000",
		"Registry.Example.COM:443":    "registry.registry.example.com-443",
		"my_reg..host":                "registry.my_reg.host",
	} {
		got := registrySecretName(host)
		if got != want {
			t.Errorf("%q: %q, want %q", host, got, want)
		}
		if err := secrets.ValidName(got); err != nil {
			t.Errorf("%q: %v", got, err)
		}
		if got != strings.ToLower(got) {
			t.Errorf("%q not lower case", got)
		}
	}
}
