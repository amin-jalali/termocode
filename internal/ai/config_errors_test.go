package ai

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	secretstore "github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/secrets/secretstest"
)

func TestConfigPathsFollowXDGAndHome(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if p, err := ConfigPath(); err != nil || p != filepath.Join(xdg, "termocode", "config.json") {
		t.Errorf("config path = %q, %v", p, err)
	}
	if p, err := CredentialsPath(); err != nil || p != filepath.Join(xdg, "termocode", "ai", "credentials.json") {
		t.Errorf("credentials path = %q, %v", p, err)
	}
	if s := DefaultChatStore(); s.Dir != filepath.Join(xdg, "termocode", "ai", "chats") {
		t.Errorf("chat dir = %q", s.Dir)
	}

	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	if p, err := ConfigPath(); err != nil || p != filepath.Join(home, ".config", "termocode", "config.json") {
		t.Errorf("home config path = %q, %v", p, err)
	}
}

// With no HOME and no XDG_CONFIG_HOME (a bare container) the paths fail
// and the chat store stays off instead of writing to "./".
func TestConfigPathsWithoutHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := ConfigPath(); err == nil {
		t.Error("ConfigPath without HOME must fail")
	}
	if _, err := CredentialsPath(); err == nil {
		t.Error("CredentialsPath without HOME must fail")
	}
	s := DefaultChatStore()
	if s.Dir != "" {
		t.Fatalf("chat dir = %q", s.Dir)
	}
	c := NewConversation()
	c.Messages = []StoredMessage{{Role: StoredUser, Text: "hi"}}
	if err := s.Save(&c); err != nil || s.List() != nil || s.Delete(c.ID) != nil {
		t.Error("a store without a folder must do nothing")
	}
	if _, err := s.Load(c.ID); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("load = %v", err)
	}
}

func TestLookupMCPSecret(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if _, ok := LookupMCPSecret("PAT"); ok {
		t.Error("no store: nothing found")
	}
	mem := secretstest.NewMemory()
	store := secretstest.New(t, mem)
	if err := store.Set("mcp.secret.PAT", "ghp_lookup_value_123"); err != nil {
		t.Fatal(err)
	}
	path, _ := CredentialsPath()
	SetSecretStoreForPath(path, store)
	t.Cleanup(func() { SetSecretStore(nil) })

	v, ok := LookupMCPSecret("PAT")
	if !ok || v != "ghp_lookup_value_123" {
		t.Fatalf("lookup = %q, %v", v, ok)
	}
	if got := RedactSecrets("token ghp_lookup_value_123 used"); got != "token [REDACTED] used" {
		t.Errorf("found secret not registered: %q", got)
	}
	if _, ok := LookupMCPSecret("OTHER"); ok {
		t.Error("missing name found")
	}
	// A store set for another credentials file is not used here.
	SetSecretStoreForPath(filepath.Join(t.TempDir(), "credentials.json"), store)
	if _, ok := LookupMCPSecret("PAT"); ok {
		t.Error("a store of another file was used")
	}
}

func TestLoadCredentialsErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadCredentials(dir); err == nil {
		t.Error("a folder in place of the file must fail")
	}
	bad := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(bad, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredentials(bad); err == nil || err.Error() != "ai/credentials.json is not valid JSON" {
		t.Errorf("corrupt file: %v", err)
	}
	// Through a store the old file is read too: corrupt is still an error.
	useStore(t, secretstest.New(t, secretstest.NewMemory()))
	if _, err := LoadCredentials(bad); err == nil {
		t.Error("corrupt file through the store must fail")
	}
	if _, err := LoadCredentials(dir); err == nil {
		t.Error("unreadable file through the store must fail")
	}
}

func TestSaveCredentialsFileErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := Credentials{Keys: map[string]string{KindOpenAI: "sk-save-err-1234567890"}}
	if err := SaveCredentials(filepath.Join(blocker, "ai", "credentials.json"), c); err == nil {
		t.Error("a file in place of the folder must fail")
	}
	// A folder where the file goes: the rename fails and no temp file stays.
	target := filepath.Join(dir, "ai", "credentials.json")
	if err := os.MkdirAll(filepath.Join(target, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(target, c); err == nil {
		t.Error("rename over a folder must fail")
	}
	ents, _ := os.ReadDir(filepath.Dir(target))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".credentials-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

// failStore is a store whose writes fail (a full disk, a keyring that
// refuses).
type failStore struct {
	secretstore.Store
	setErr, delErr error
}

func (f failStore) Set(n, v string) error {
	if f.setErr != nil {
		return f.setErr
	}
	return f.Store.Set(n, v)
}

func (f failStore) Delete(n string) error {
	if f.delErr != nil {
		return f.delErr
	}
	return f.Store.Delete(n)
}

func TestSaveCredentialsStoreWriteErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	base := secretstest.New(t, secretstest.NewMemory())
	if err := base.Set("ai.key.openai", "sk-old-key-1234567890"); err != nil {
		t.Fatal(err)
	}
	disk := errors.New("disk full")

	useStore(t, failStore{Store: base, delErr: disk})
	if err := SaveCredentials(path, Credentials{}); !errors.Is(err, disk) {
		t.Errorf("delete error not returned: %v", err)
	}
	useStore(t, failStore{Store: base, setErr: disk})
	if err := SaveCredentials(path, Credentials{Keys: map[string]string{KindOpenAI: "sk-new-key-1234567890"}}); !errors.Is(err, disk) {
		t.Errorf("set error not returned: %v", err)
	}
	if v, _, _ := base.Get("ai.key.openai"); v != "sk-old-key-1234567890" {
		t.Errorf("old key lost: %q", v)
	}
}

// A key only in the old credentials.json moves into the store on load.
func TestLoadCredentialsMovesOldFileValuesIntoStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(`{"keys":{"openai":"sk-file-only-1234567890"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := secretstest.NewMemory()
	store := secretstest.New(t, mem)
	useStore(t, store)
	c, err := LoadCredentials(path)
	if err != nil || c.Keys[KindOpenAI] != "sk-file-only-1234567890" {
		t.Fatalf("load = %+v, %v", c, err)
	}
	if v, ok, _ := store.Get("ai.key.openai"); !ok || v != "sk-file-only-1234567890" {
		t.Errorf("store = %q, %v", v, ok)
	}
	// A store that refuses the copy still returns the file's value.
	path2 := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path2, []byte(`{"keys":{"anthropic":"sk-ant-file-only-1234"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	useStore(t, failStore{Store: secretstest.New(t, secretstest.NewMemory()), setErr: errors.New("no")})
	if c, err := LoadCredentials(path2); err != nil || c.Keys[KindAnthropic] != "sk-ant-file-only-1234" {
		t.Errorf("load with refusing store = %+v, %v", c, err)
	}
}

func TestResolveEnvironmentChoices(t *testing.T) {
	cases := []struct {
		name string
		s    Settings
		c    Credentials
		env  map[string]string
		kind string
		key  string
		url  string
	}{
		{name: "anthropic env key picks anthropic", env: map[string]string{"ANTHROPIC_API_KEY": "sk-ant-env-12345678"}, kind: KindAnthropic, key: "sk-ant-env-12345678"},
		{name: "stored anthropic key", c: Credentials{Keys: map[string]string{KindAnthropic: "sk-ant-file-12345678"}}, kind: KindAnthropic, key: "sk-ant-file-12345678"},
		{name: "stored openai key", c: Credentials{Keys: map[string]string{KindOpenAI: "sk-file-openai-12345678"}}, kind: KindOpenAI, key: "sk-file-openai-12345678"},
		{name: "termocode key for the set kind", s: Settings{Provider: KindOpenRouter}, env: map[string]string{"TERMOCODE_AI_API_KEY": "tk-12345678"}, kind: KindOpenRouter, key: "tk-12345678"},
		{name: "base url env wins", s: Settings{Provider: KindOllama, BaseURL: "http://set"}, env: map[string]string{"TERMOCODE_AI_BASE_URL": "http://env/v1", "OLLAMA_HOST": "box:1"}, kind: KindOllama, url: "http://env/v1"},
		{name: "nothing set", kind: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := Resolve(c.s, c.c, envMap(c.env))
			if cfg.Kind != c.kind || cfg.APIKey != c.key || (c.url != "" && cfg.BaseURL != c.url) {
				t.Errorf("cfg = %+v", cfg)
			}
		})
	}
}

func TestResolveReadsProcessEnvironment(t *testing.T) {
	for _, k := range []string{"TERMOCODE_AI_PROVIDER", "TERMOCODE_AI_MODEL", "TERMOCODE_AI_BASE_URL", "TERMOCODE_AI_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OLLAMA_HOST"} {
		t.Setenv(k, "")
	}
	t.Setenv("OPENAI_API_KEY", "sk-process-env-12345678")
	if cfg := Resolve(Settings{}, Credentials{}, nil); cfg.Kind != KindOpenAI || cfg.APIKey != "sk-process-env-12345678" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestRedactLongestSecretFirst(t *testing.T) {
	RegisterSecret("overlap-secret")
	RegisterSecret("overlap-secret-longer")
	for name, f := range map[string]func(string) string{"Redact": Redact, "RedactSecrets": RedactSecrets} {
		if got := f("x overlap-secret-longer y"); got != "x [REDACTED] y" {
			t.Errorf("%s = %q", name, got)
		}
		if f("") != "" {
			t.Errorf("%s of empty text", name)
		}
	}
}

func TestSettingsReviewEditsAndPlanApproval(t *testing.T) {
	if !(Settings{}).ReviewEdits() || (Settings{EditReview: EditReviewAuto}).ReviewEdits() {
		t.Error("edit review: default review, auto off")
	}
	for in, want := range map[string]string{"": PlanApprovalAsk, "junk": PlanApprovalAsk, PlanApprovalAuto: PlanApprovalAuto, PlanApprovalAlways: PlanApprovalAlways} {
		if got := (Settings{PlanApproval: in}).PlanApprovalMode(); got != want {
			t.Errorf("plan approval %q = %q, want %q", in, got, want)
		}
	}
}
