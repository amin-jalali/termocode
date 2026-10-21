package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/debounce"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
)

const (
	secretsTestPass = "correct horse battery"
	secretsTestKey  = "sk-ss3-openai-key-123456"
)

// resetSecretStore forgets the app's store, so the next use opens it
// again for the current config dir and setting.
func resetSecretStore() {
	secretStoreConfig.Lock()
	if s := secretStoreConfig.store; s != nil {
		s.Lock()
	}
	secretStoreConfig.key, secretStoreConfig.store, secretStoreConfig.err = "", nil, nil
	secretStoreConfig.Unlock()
	ai.SetSecretStore(nil)
}

// withSecretSetting gives a test its own config dir with secret_store =
// setting ("" = not set), reads the real setting (not TestMain's plain
// override), a fast age work factor and a keyring probe that answers
// probe (and counts its calls). It returns the termocode config dir.
func withSecretSetting(t *testing.T, setting string, probe bool) (dir string, probes *int) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "TERMOCODE_AI_PROVIDER", "TERMOCODE_AI_API_KEY", "TERMOCODE_AI_MODEL", "TERMOCODE_AI_BASE_URL", "OLLAMA_HOST"} {
		t.Setenv(k, "")
	}
	dir = filepath.Join(root, "termocode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"ai_provider": "openai"}
	if setting != "" {
		cfg["secret_store"] = setting
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	n := 0
	oldOverride, oldProbe, oldWork := secretStoreSettingOverride, secretKeyringProbe, secretScryptWork
	secretStoreSettingOverride = ""
	secretKeyringProbe = func() bool { n++; return probe }
	secretScryptWork = 14
	resetSecretStore()
	t.Cleanup(func() {
		resetSecretStore()
		secretStoreSettingOverride, secretKeyringProbe, secretScryptWork = oldOverride, oldProbe, oldWork
	})
	return dir, &n
}

// writeAgeStore makes secrets.age with the OpenAI key behind
// secretsTestPass, as an earlier session left it.
func writeAgeStore(t *testing.T, dir string) {
	t.Helper()
	s, err := secrets.Open(secrets.Options{Dir: dir, Backend: "age", ScryptWorkFactor: 14})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.openai", secretsTestKey); err != nil {
		t.Fatal(err)
	}
	s.Lock()
}

func secretsTestModel(t *testing.T) *Model {
	t.Helper()
	// Not aiTestModel: it would move XDG_CONFIG_HOME away from the
	// test's store.
	m := Model{w: 120, h: 40, debounce: debounce.New(), ai: newAIState(), actionsWidth: defaultActionsWidth, secrets: &secretsState{}}
	return &m
}

// submitSecrets types v into the open passphrase prompt and presses
// Enter through Update, then pumps the answer like Bubble Tea would: the
// unlock command runs on its own goroutine and its message comes back.
func submitSecrets(t *testing.T, m *Model, v string) {
	t.Helper()
	if !m.secretsPromptOpen() {
		t.Fatalf("no passphrase prompt open (kind %v)", m.promptKind)
	}
	next, cmd := m.update(prompt.SubmitMsg{Value: v})
	*m = next
	pumpSecrets(t, m, cmd)
}

// pumpSecrets runs a command tree and feeds secretsUnlockDoneMsg (and
// the migration's messages, secrets_migrate.go) back to Update. Commands that wait (toast and animation ticks) are dropped
// after a short wait; while an unlock runs (scrypt, slow under -race) it
// waits for that answer.
func pumpSecrets(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	results := make(chan tea.Msg, 64)
	running := 0
	start := func(c tea.Cmd) {
		if c != nil {
			running++
			go func() { results <- c() }()
		}
	}
	start(cmd)
	for running > 0 {
		wait := 100 * time.Millisecond
		if m.secrets.busy {
			wait = 30 * time.Second
		}
		select {
		case got := <-results:
			running--
			switch msg := got.(type) {
			case tea.BatchMsg:
				for _, c := range msg {
					start(c)
				}
			case secretsUnlockDoneMsg, secretsMigrateCheckMsg, secretsMigrateDoneMsg:
				next, more := m.update(msg)
				*m = next
				start(more)
			}
		case <-time.After(wait):
			if m.secrets.busy {
				t.Fatal("the unlock never answered")
			}
			return
		}
	}
}

func storeLocked(t *testing.T) bool {
	t.Helper()
	s, err := configureSecretStore()
	if err != nil {
		t.Fatal(err)
	}
	return s.Locked()
}

// ── Backend selection (plan §3.3) ─────────────────────────────────────

func TestSecretStoreAutoPicksAgeOrKeyring(t *testing.T) {
	dir, probes := withSecretSetting(t, "", false)
	s, err := configureSecretStore()
	if err != nil || s.Backend() != "age" || *probes != 1 {
		t.Fatalf("no keyring: backend %v err %v probes %d", s, err, *probes)
	}
	var idx secrets.Index
	b, _ := os.ReadFile(filepath.Join(dir, secrets.IndexFile))
	if json.Unmarshal(b, &idx) != nil || idx.Backend != "age" {
		t.Fatalf("choice not recorded: %s", b)
	}
	// A recorded choice is not probed again.
	resetSecretStore()
	if s, _ := configureSecretStore(); s.Backend() != "age" || *probes != 1 {
		t.Fatalf("second start: %s probes %d", s.Backend(), *probes)
	}

	_, probes = withSecretSetting(t, "auto", true)
	if s, err := configureSecretStore(); err != nil || s.Backend() != "keyring" || *probes != 1 {
		t.Fatalf("keyring answers: %v %v %d", s, err, *probes)
	}
}

// Backward compatibility: a plain credentials.json from before SS-3 keeps
// working under auto, with no prompt and no keyring probe.
func TestSecretStoreKeepsExistingPlainFile(t *testing.T) {
	dir, probes := withSecretSetting(t, "", true)
	cred := filepath.Join(dir, "ai", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(cred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte(`{"keys":{"openai":"`+secretsTestKey+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := secretsTestModel(t)
	m.ai.reload()
	s, _ := configureSecretStore()
	if s.Backend() != "plain" || *probes != 0 {
		t.Fatalf("backend %s probes %d", s.Backend(), *probes)
	}
	if _, ok := m.aiReady(); !ok || m.secretsPromptOpen() {
		t.Fatal("a plain user lost the AI key or got a prompt")
	}
	if b, _ := os.ReadFile(cred); !strings.Contains(string(b), secretsTestKey) {
		t.Fatalf("credentials.json changed: %s", b)
	}
}

// secret_store = keyring with no keyring: an error, never a silent
// fallback to the plain file.
func TestSecretStoreForcedKeyringFailsLoudly(t *testing.T) {
	dir, _ := withSecretSetting(t, "keyring", false)
	if _, err := configureSecretStore(); !errors.Is(err, secrets.ErrKeyringUnavailable) {
		t.Fatalf("err = %v", err)
	}
	cred := filepath.Join(dir, "ai", "credentials.json")
	if err := ai.SaveCredentials(cred, ai.Credentials{Keys: map[string]string{"openai": secretsTestKey}}); err == nil {
		t.Fatal("a save went through without a store")
	}
	if _, err := os.Stat(cred); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fell back to the plain file: %v", err)
	}
	m := secretsTestModel(t)
	m.dispatchPaletteAction("secrets-unlock")
	if m.secretsPromptOpen() || !strings.Contains(toastText(*m), "Secret store not available") {
		t.Fatalf("toast: %q", toastText(*m))
	}
}

// AI, MCP and the Databases view read one store.
func TestSecretStoreIsShared(t *testing.T) {
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	s, _ := configureSecretStore()
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	cred, _ := ai.CredentialsPath()
	c, err := ai.LoadCredentials(cred)
	if err != nil || c.Keys["openai"] != secretsTestKey {
		t.Fatalf("ai view: %+v %v", c.Keys, err)
	}
	c.MCPSecrets = map[string]string{"GITHUB_PAT": "ghp_ss3_shared_value"}
	if err := ai.SaveCredentials(cred, c); err != nil {
		t.Fatal(err)
	}
	if v, ok := ai.LookupMCPSecret("GITHUB_PAT"); !ok || v != "ghp_ss3_shared_value" {
		t.Fatalf("mcp lookup = %q %v", v, ok)
	}
	if databaseSecretStore() != s {
		t.Fatal("the Databases view has another store")
	}
	if _, err := os.Stat(cred); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an age store wrote credentials.json")
	}
}

// ── Unlock / lock ─────────────────────────────────────────────────────

func TestSecretsUnlockFlow(t *testing.T) {
	dir, _ := withSecretSetting(t, "", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	m.ai.reload()
	if m.aiConfigured() {
		t.Fatal("configured while locked")
	}
	m.dispatchPaletteAction("secrets-unlock")
	if !m.secretsPromptOpen() || !m.prompt.Masked() {
		t.Fatal("no masked passphrase prompt")
	}
	// Under the floor: refused, not counted.
	submitSecrets(t, m, "short")
	if !m.secretsPromptOpen() || !strings.Contains(m.prompt.Note(), "at least 8") {
		t.Fatalf("short: note %q", m.prompt.Note())
	}
	// Unlock runs off the UI goroutine: Enter only returns a command.
	next, cmd := m.update(prompt.SubmitMsg{Value: "wrong passphrase one"})
	*m = next
	if cmd == nil || !storeLocked(t) || !m.secrets.busy {
		t.Fatal("unlock ran on the UI goroutine")
	}
	pumpSecrets(t, m, cmd)
	if !m.secretsPromptOpen() || !strings.Contains(m.prompt.Note(), "2 tries left") {
		t.Fatalf("first wrong: %q", m.prompt.Note())
	}
	submitSecrets(t, m, "wrong passphrase two")
	if !strings.Contains(m.prompt.Note(), "1 try left") {
		t.Fatalf("second wrong: %q", m.prompt.Note())
	}
	submitSecrets(t, m, "wrong passphrase three")
	if m.secretsPromptOpen() || !storeLocked(t) || !strings.Contains(toastText(*m), "Secrets stay locked") {
		t.Fatalf("third wrong: open=%v toast %q", m.secretsPromptOpen(), toastText(*m))
	}
	if m.aiConfigured() {
		t.Fatal("configured after three wrong tries")
	}
	// The palette entry tries again.
	m.dispatchPaletteAction("secrets-unlock")
	submitSecrets(t, m, secretsTestPass)
	if storeLocked(t) || !m.aiConfigured() || !strings.Contains(toastText(*m), "Secrets unlocked") {
		t.Fatalf("unlock: locked=%v ai=%v toast %q", storeLocked(t), m.aiConfigured(), toastText(*m))
	}
	if m.prompt.Value() != "" {
		t.Fatal("the passphrase stayed in the prompt")
	}
	m.dispatchPaletteAction("secrets-lock")
	if !storeLocked(t) || m.aiConfigured() || !strings.Contains(toastText(*m), "Secrets locked") {
		t.Fatalf("lock: locked=%v ai=%v", storeLocked(t), m.aiConfigured())
	}
	for _, v := range []string{secretsTestPass, "wrong passphrase", secretsTestKey} {
		if strings.Contains(toastText(*m), v) {
			t.Fatalf("toast shows a secret: %q", toastText(*m))
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "errors.log")); strings.Contains(string(b), "passphrase one") || strings.Contains(string(b), secretsTestPass) {
		t.Fatalf("errors.log holds a passphrase: %s", b)
	}
}

// The first AI entry point asks once per process; Esc stops asking until
// AI: Lock Secrets (or the palette entry).
func TestSecretsAskOnFirstNeedOnce(t *testing.T) {
	dir, _ := withSecretSetting(t, "", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	m.ai.reload()
	if _, ok := m.aiReady(); ok || !m.secretsPromptOpen() {
		t.Fatal("first need did not ask")
	}
	next, _ := m.update(prompt.CloseMsg{})
	*m = next
	if _, ok := m.aiReady(); ok || m.secretsPromptOpen() {
		t.Fatal("asked twice in one process")
	}
	if !strings.Contains(toastText(*m), "locked") {
		t.Fatalf("not configured toast does not say locked: %q", toastText(*m))
	}
	m.dispatchPaletteAction("secrets-unlock")
	submitSecrets(t, m, secretsTestPass)
	if _, ok := m.aiReady(); !ok {
		t.Fatal("not ready after unlock")
	}
	m.dispatchPaletteAction("secrets-lock")
	if _, ok := m.aiReady(); ok || !m.secretsPromptOpen() {
		t.Fatal("after Lock the next need does not ask again")
	}
}

// A brand-new setup has no age file: nothing is asked until a secret is
// saved; then a new passphrase is typed twice (floor 8, warning under
// 12) and the waiting step continues.
func TestSecretsCreateOnFirstSave(t *testing.T) {
	dir, _ := withSecretSetting(t, "", false)
	m := secretsTestModel(t)
	m.ai.reload()
	if _, ok := m.aiReady(); ok || m.secretsPromptOpen() {
		t.Fatal("asked for a passphrase with nothing stored")
	}
	m.openAIKeyPrompt(ai.KindOpenAI)
	if !m.secretsPromptOpen() || !strings.Contains(m.prompt.Note(), "at least 8") {
		t.Fatalf("no new-passphrase prompt: %q", m.prompt.Note())
	}
	submitSecrets(t, m, "seven77")
	if !strings.Contains(m.prompt.Note(), "Too short") {
		t.Fatalf("floor: %q", m.prompt.Note())
	}
	submitSecrets(t, m, "eleven-char")
	if !strings.Contains(m.prompt.Note(), "Weak") {
		t.Fatalf("no weak warning: %q", m.prompt.Note())
	}
	submitSecrets(t, m, "eleven-chaX")
	if !strings.Contains(m.prompt.Note(), "differ") {
		t.Fatalf("mismatch: %q", m.prompt.Note())
	}
	if _, err := os.Stat(filepath.Join(dir, secrets.AgeFile)); err == nil {
		t.Fatal("a store was made before the confirm")
	}
	submitSecrets(t, m, secretsTestPass)
	submitSecrets(t, m, secretsTestPass)
	if _, err := os.Stat(filepath.Join(dir, secrets.AgeFile)); err != nil {
		t.Fatalf("no secrets.age: %v", err)
	}
	if !m.promptOpen || m.promptKind != promptKindAIKey {
		t.Fatalf("the key prompt did not follow (kind %v)", m.promptKind)
	}
	next, _ := m.update(prompt.SubmitMsg{Value: secretsTestKey})
	*m = next
	if !m.aiConfigured() {
		t.Fatal("key not saved through the new store")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, secrets.AgeFile)); strings.Contains(string(b), secretsTestKey) {
		t.Fatal("secrets.age holds the key in clear")
	}
}

func TestSecretsLockWithoutPassphraseStore(t *testing.T) {
	dir, _ := withSecretSetting(t, "plain", false)
	_ = dir
	m := secretsTestModel(t)
	m.dispatchPaletteAction("secrets-lock")
	if !strings.Contains(toastText(*m), "Nothing to lock") {
		t.Fatalf("toast %q", toastText(*m))
	}
}

// ── Policies, settings, setup card ─────────────────────────────────────

func TestSecretsNeverForTheAI(t *testing.T) {
	ids := map[string]string{}
	for _, e := range PaletteCatalog() {
		ids[e.ID] = e.Title
	}
	for id, title := range map[string]string{"secrets-unlock": "AI: Unlock Secrets", "secrets-lock": "AI: Lock Secrets"} {
		if ids[id] != title {
			t.Errorf("palette %s = %q, want %q", id, ids[id], title)
		}
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever || p.Reason == "" {
			t.Errorf("%s: policy %+v", id, p)
		}
	}
	keys := map[string]SettingEntry{}
	for _, s := range SettingsCatalog() {
		keys[s.Key] = s
	}
	if e := keys["secret_store"]; e.Default != "auto" || strings.Join(e.Choices, ",") != "auto,keyring,age,plain" || e.Description == "" {
		t.Errorf("secret_store entry %+v", e)
	}
	if e := keys["secret_lock_minutes"]; e.Default != "0" || e.Type != "int" || e.Description == "" {
		t.Errorf("secret_lock_minutes entry %+v", e)
	}
	for _, k := range []string{"secret_store", "secret_lock_minutes"} {
		if p, ok := aiSettingPolicies[k]; !ok || p.Gate != aiGateNever || p.Reason != aiNoAIConfig {
			t.Errorf("%s: policy %+v", k, p)
		}
	}
}

// settings_set from the model is refused without asking the user.
func TestSecretsSettingsSetRefused(t *testing.T) {
	for _, k := range []string{"secret_store", "secret_lock_minutes"} {
		p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("settings_set", map[string]any{"key": k, "value": "plain"})}}
		m, _ := aiIDETestModel(t, p)
		asked := 0
		driveAIChat(t, m, m.sendAIChat("set"), func(aiChatConfirmMsg) string { asked++; return "allow" })
		if r := p.toolResults(); asked != 0 || len(r) != 1 || !strings.Contains(r[0], "not for the AI") {
			t.Fatalf("%s: asked=%d results %v", k, asked, r)
		}
		if strings.Contains(loadSettings().SecretStore, "plain") {
			t.Fatalf("%s: the setting changed", k)
		}
	}
}

func TestSetupAIStepLocked(t *testing.T) {
	f := baseFacts(kittyInfo)
	f.secretsLocked = true
	s := stepByID(t, buildSetupSteps(f), "ai")
	if s.done || !strings.Contains(s.status, "locked") {
		t.Fatalf("locked row: %+v", s)
	}
	if _, ok := buttonByID(s, "secrets-unlock"); !ok {
		t.Fatal("no Unlock button")
	}
	// The model's facts see the locked store.
	dir, _ := withSecretSetting(t, "", false)
	writeAgeStore(t, dir)
	m := secretsTestModel(t)
	m.setup = newSetupState()
	m.ai.reload()
	if !m.setupFacts().secretsLocked {
		t.Fatal("setup facts miss the locked store")
	}
}

// Signing in to a service or setting an MCP secret saves a secret: a
// locked (or not yet created) encrypted store asks first, not after the
// browser sign-in failed with "secrets are locked".
func TestSecretsAskBeforeMCPSaves(t *testing.T) {
	for name, open := range map[string]func(*Model) tea.Cmd{
		"connect":   func(m *Model) tea.Cmd { return m.openMCPConnect() },
		"server":    func(m *Model) tea.Cmd { return m.mcpConnectServer("lin") },
		"mcpSecret": func(m *Model) tea.Cmd { return m.mcpAskSecret("GITHUB_PAT") },
	} {
		withSecretSetting(t, "", false)
		m := secretsTestModel(t)
		open(m)
		if !m.secretsPromptOpen() {
			t.Fatalf("%s: no passphrase prompt first", name)
		}
	}
}
