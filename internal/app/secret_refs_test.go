package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── SS-5: references in secret prompts ─────────────────────────────────

// refTestSetup gives a test its own config dir (plain backend, so the
// resolved value would be visible in credentials.json if it leaked) and a
// clean first-use notice state.
func refTestSetup(t *testing.T) (dir string, m *Model) {
	t.Helper()
	dir, _ = withSecretSetting(t, "plain", false)
	secrets.ResetCmdNotices()
	cmdNotices.Lock()
	cmdNotices.texts = nil
	cmdNotices.Unlock()
	t.Cleanup(func() {
		secrets.ResetCmdNotices()
		cmdNotices.Lock()
		cmdNotices.texts = nil
		cmdNotices.Unlock()
	})
	return dir, secretsTestModel(t)
}

// refIndex is the text of secrets.json (the index: names and references).
func refIndex(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, secrets.IndexFile))
	if err != nil {
		t.Fatalf("no index: %v", err)
	}
	return string(b)
}

// refNames are the secret names the app's store holds.
func refNames(t *testing.T) []string {
	t.Helper()
	st, err := configureSecretStore()
	if err != nil {
		t.Fatal(err)
	}
	return st.Names()
}

// refNowhereOnDisk fails when the resolved value is in any file under dir.
func refNowhereOnDisk(t *testing.T, dir, value string) {
	t.Helper()
	noSecretOnDisk(t, value, filepath.Dir(dir))
}

func TestSecretPromptsShowTheReferenceHint(t *testing.T) {
	_, m := refTestSetup(t)
	m.openAIKeyPrompt("openai")
	checks := map[string]string{"ai key": m.prompt.Note()}
	m.mcpAskSecret("PAT")
	checks["mcp secret"] = m.prompt.Note()
	m.databases = newDBState()
	m.databaseAddPrompt(promptKindDBPassword, "Database password", "Password:", "", true)
	checks["db password"] = m.prompt.Note()
	m.databaseAddPrompt(promptKindDBSSHSecret, "SSH tunnel · password", "SSH password:", "", true)
	checks["db ssh secret"] = m.prompt.Note()
	m.databaseSetPasswordPrompt("x")
	checks["db set password"] = m.prompt.Note()
	for name, note := range checks {
		if !strings.Contains(note, secrets.PromptHint) {
			t.Errorf("%s: note %q lacks %q", name, note, secrets.PromptHint)
		}
	}
	// And the rendered card shows it.
	if box := ansi.Strip(m.prompt.View()); !strings.Contains(box, "${env:NAME}") {
		t.Errorf("card does not show the hint:\n%s", box)
	}
}

func TestRegistryTokenPromptShowsTheReferenceHint(t *testing.T) {
	m, _, _ := regSession(t, regConfig)
	m.askRegistryToken()
	if !strings.Contains(m.prompt.Note(), secrets.PromptHint) {
		t.Fatalf("note %q lacks the hint", m.prompt.Note())
	}
}

func TestAIKeyPromptStoresReferenceAsText(t *testing.T) {
	dir, m := refTestSetup(t)
	const resolved = "sk-ss5-resolved-value-123456"
	t.Setenv("SS5_AI_KEY", resolved)
	m.openAIKeyPrompt("openai")
	next, _ := m.update(prompt.SubmitMsg{Value: "${env:SS5_AI_KEY}"})
	*m = next
	idx := refIndex(t, dir)
	if !strings.Contains(idx, "${env:SS5_AI_KEY}") || strings.Contains(idx, resolved) {
		t.Fatalf("index must hold the reference text only:\n%s", idx)
	}
	refNowhereOnDisk(t, dir, resolved)
	if !m.aiConfigured() {
		t.Fatal("the reference does not resolve to a key at use time")
	}
}

func TestMCPSecretPromptStoresReferenceAsText(t *testing.T) {
	dir, m := refTestSetup(t)
	const resolved = "ghp_ss5_resolved_value_123456"
	t.Setenv("SS5_PAT", resolved)
	m.mcpAskSecret("PAT")
	next, _ := m.update(prompt.SubmitMsg{Value: "${env:SS5_PAT}"})
	*m = next
	idx := refIndex(t, dir)
	if !strings.Contains(idx, "mcp.secret.PAT") || !strings.Contains(idx, "${env:SS5_PAT}") || strings.Contains(idx, resolved) {
		t.Fatalf("index:\n%s", idx)
	}
	refNowhereOnDisk(t, dir, resolved)
	if v, ok := ai.LookupMCPSecret("PAT"); !ok || v != resolved {
		t.Fatalf("lookup = %q %v, want the resolved value", v, ok)
	}
}

func TestDBPasswordPromptStoresReferenceAsText(t *testing.T) {
	dir, m := refTestSetup(t)
	const resolved = "db-ss5-resolved-pw-123456"
	t.Setenv("SS5_DB_PW", resolved)
	s := newDBState()
	s.root = t.TempDir()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres", Host: "127.0.0.1", Port: 9, Source: dbclient.SourceUser}}
	s.nodes["pg"] = dbNode{kind: dbNodeConnection, name: "pg", path: "pg", conn: "pg"}
	m.databases = s
	s.pendingSecretID = "pg"
	m.promptKind = promptKindDBPassword
	m.handlePromptSubmit("${env:SS5_DB_PW}")
	idx := refIndex(t, dir)
	if !strings.Contains(idx, `"db.pg"`) || !strings.Contains(idx, "${env:SS5_DB_PW}") || strings.Contains(idx, resolved) {
		t.Fatalf("index:\n%s", idx)
	}
	refNowhereOnDisk(t, dir, resolved)
}

// The add flow tests the connection with the resolved value and saves
// the reference text, for the database password and the SSH secrets.
func TestDBAddFlowSavesReferenceTextNotValue(t *testing.T) {
	dir, m := refTestSetup(t)
	const pw, sshpw = "db-ss5-resolved-pw-123456", "ssh-ss5-resolved-pw-123456"
	t.Setenv("SS5_DB_PW", pw)
	t.Setenv("SS5_SSH_PW", sshpw)
	s := newDBState()
	s.root = t.TempDir()
	m.databases = s
	s.pendingConn = dbclient.Conn{ID: "new", Name: "new", Driver: "postgres", Host: "127.0.0.1", Port: 9, Database: "app", User: "u",
		SSH: &dbclient.SSHConfig{Host: "bastion", User: "ops", Auth: dbclient.SSHAuthPassword}}
	m.promptKind = promptKindDBPassword
	m.handlePromptSubmit("${env:SS5_DB_PW}")
	if s.pendingPassword != pw {
		t.Fatalf("the test must use the resolved value, got %q", s.pendingPassword)
	}
	s.secretAsk = &dbSecretAsk{kind: "password", then: dbThenPending}
	m.databaseSSHSecretTyped("${env:SS5_SSH_PW}")
	if s.pendingSSH.password != sshpw {
		t.Fatalf("the tunnel must use the resolved value, got %q", s.pendingSSH.password)
	}
	m.databaseSavePending(false)
	idx := refIndex(t, dir)
	for _, want := range []string{`"db.new"`, "${env:SS5_DB_PW}", `"db.new.ssh"`, "${env:SS5_SSH_PW}"} {
		if !strings.Contains(idx, want) {
			t.Errorf("index lacks %s:\n%s", want, idx)
		}
	}
	if strings.Contains(idx, pw) || strings.Contains(idx, sshpw) {
		t.Fatalf("index holds a resolved value:\n%s", idx)
	}
	refNowhereOnDisk(t, dir, pw)
	refNowhereOnDisk(t, dir, sshpw)
	// A later change of the typed text must not be saved as a stale reference.
	if got := (dbTypedRef{text: "${env:X}", value: "old"}).textFor("new"); got != "new" {
		t.Errorf("textFor after a change = %q", got)
	}
}

func TestMCPConnectTokenStepAcceptsReferenceAndRefusesBadOnes(t *testing.T) {
	dir, m := refTestSetup(t)
	t.Setenv("SS5_TOK", "tok-ss5-resolved-value-123456")
	w := &mcpWizard{preset: mcp.Preset{Name: "custom", Title: "Custom", URL: "https://x.example/mcp"}, name: "custom", step: wizToken}
	m.aiS().mcpWiz = w
	if got := wizText(m.wizCard()); !strings.Contains(got, secrets.PromptHint) {
		t.Fatalf("token card lacks the hint:\n%s", got)
	}
	m.onMCPWizSubmit("token", "${foo:x}")
	if w.pasteErr == "" || strings.Contains(w.pasteErr, "foo") {
		t.Fatalf("pasteErr = %q, want a fixed refusal", w.pasteErr)
	}
	if n := refNames(t); len(n) != 0 {
		t.Fatalf("a refused reference was saved: %v", n)
	}
	if got := wizText(m.wizCard()); !strings.Contains(got, w.pasteErr) {
		t.Fatalf("the refusal is not on the card:\n%s", got)
	}
	m.onMCPWizSubmit("token", "${env:SS5_TOK}")
	idx := refIndex(t, dir)
	if !strings.Contains(idx, "mcp.secret.custom_token") || !strings.Contains(idx, "${env:SS5_TOK}") || strings.Contains(idx, "tok-ss5-resolved") {
		t.Fatalf("index:\n%s", idx)
	}
}

// A token that is joined to an e-mail before it is stored cannot be a reference.
func TestMCPConnectEmailTokenRefusesReference(t *testing.T) {
	_, m := refTestSetup(t)
	w := &mcpWizard{preset: mcp.Preset{Name: "custom", Title: "Custom", URL: "https://x.example/mcp", Token: &mcp.TokenRecipe{Email: true}},
		name: "custom", step: wizToken, email: "a@b.example"}
	m.aiS().mcpWiz = w
	m.onMCPWizSubmit("token", "${env:SS5_TOK}")
	if w.pasteErr == "" {
		t.Fatal("a reference was accepted where the token is encoded with the e-mail")
	}
	if n := refNames(t); len(n) != 0 {
		t.Fatalf("saved: %v", n)
	}
}

func wizText(c connectcard.Card) string {
	var b strings.Builder
	for _, r := range c.Rows {
		b.WriteString(r.Text + "\n")
	}
	return b.String()
}

// Every secret prompt refuses a malformed reference in the prompt: it
// stays open, shows a fixed message and saves nothing.
func TestSecretPromptsRefuseMalformedReference(t *testing.T) {
	kinds := map[string]promptKindEnum{
		"ai key": promptKindAIKey, "mcp secret": promptKindMCPSecret, "db password": promptKindDBPassword,
		"db ssh secret": promptKindDBSSHSecret, "registry token": promptKindRegistryToken,
	}
	for name, kind := range kinds {
		for _, bad := range []string{"${env:}", "${foo:x}", "${cmd:echo hidden-part", "${dotenv:}", "${cmd:}"} {
			_, m := refTestSetup(t)
			m.aiS().cfgKind, m.aiS().mcpSecret = "openai", "PAT"
			m.prompt = prompt.New("t", "l", "")
			m.prompt.SetSize(m.w, m.h)
			m.promptOpen, m.promptKind = true, kind
			next, cmd := m.update(prompt.SubmitMsg{Value: bad})
			*m = next
			if cmd != nil {
				t.Errorf("%s %q: a command ran", name, bad)
			}
			if !m.promptOpen || m.prompt.Error() == "" {
				t.Errorf("%s %q: prompt open=%v error=%q", name, bad, m.promptOpen, m.prompt.Error())
			}
			if strings.Contains(m.prompt.Error(), "hidden-part") || strings.Contains(m.prompt.Error(), "foo") {
				t.Errorf("%s %q: the message quotes the typed text: %q", name, bad, m.prompt.Error())
			}
			if n := refNames(t); len(n) != 0 {
				t.Errorf("%s %q: something was saved: %v", name, bad, n)
			}
			if m.aiConfigured() {
				t.Errorf("%s %q: a key was saved", name, bad)
			}
			if !strings.Contains(ansi.Strip(m.prompt.View()), "Not a valid reference") {
				t.Errorf("%s %q: the card does not show the refusal", name, bad)
			}
		}
	}
}

// Plain values (including text that merely contains "${") are not references.
func TestSecretPromptsAcceptPlainValues(t *testing.T) {
	_, m := refTestSetup(t)
	m.openAIKeyPrompt("openai")
	m.promptOpen = true
	if m.refusePromptSecret("sk-plain-value-123456") || m.refusePromptSecret("pa$$word{x}") {
		t.Fatal("a plain value was refused")
	}
}

// ${cmd:} first use: a toast "Running: <command text>" once per command
// text, never the output.
func TestCmdReferenceToastOncePerCommandNeverOutput(t *testing.T) {
	_, m := refTestSetup(t)
	const out = "ss5-cmd-output-value-98765"
	line := "printf '%s-%s\\n' ss5-cmd-output value-98765"
	submit := func() {
		m.openAIKeyPrompt("openai")
		model, _ := m.Update(prompt.SubmitMsg{Value: "${cmd:" + line + "}"})
		*m = model.(Model)
		// A later message shows what a background resolve queued.
		model, _ = m.Update(frameTickMsg{})
		*m = model.(Model)
	}
	submit()
	got := toastText(*m)
	if strings.Count(got, "Running: "+line) != 1 {
		t.Fatalf("want one 'Running: %s' toast, got:\n%s", line, got)
	}
	if strings.Contains(got, out) {
		t.Fatalf("the toast shows the output:\n%s", got)
	}
	// Lock forgets the cached output: the command runs again, the toast does not.
	m.toast = toast.Model{}
	secretStoreConfig.Lock()
	secretStoreConfig.store.Lock()
	secretStoreConfig.Unlock()
	submit()
	if strings.Contains(toastText(*m), "Running:") {
		t.Fatalf("the same command text was announced twice:\n%s", toastText(*m))
	}
	// A new command text is announced.
	m.openAIKeyPrompt("openai")
	other := "printf '%s-%s\\n' ss5-other value-55555"
	model, _ := m.Update(prompt.SubmitMsg{Value: "${cmd:" + other + "}"})
	*m = model.(Model)
	if !strings.Contains(toastText(*m), "Running: "+other) {
		t.Fatalf("a new command was not announced:\n%s", toastText(*m))
	}
}

// A project connection with ${cmd:} is rejected and the command is not run.
func TestProjectDatabaseCmdReferenceNeverRuns(t *testing.T) {
	dir, m := refTestSetup(t)
	_ = dir
	root := t.TempDir()
	marker := filepath.Join(root, "ran-marker")
	cfg := filepath.Join(root, ".termocode", "databases.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"connections":[{"id":"evil","driver":"postgres","host":"127.0.0.1","port":9,"database":"a","user":"u","password":"${cmd:touch ` + marker + `; echo pw-value-1234567}"}]}`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newDBState()
	s.load(root)
	m.databases = s
	if n := s.nodes["evil"]; !strings.Contains(n.problem, "${cmd:}") {
		t.Fatalf("node problem = %q, want the refusal", n.problem)
	}
	m.databaseOpen("evil", false)
	m.databaseOpen("evil", true)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the project's ${cmd:} ran")
	}
	if !strings.Contains(toastText(*m), "${cmd:}") {
		t.Errorf("no clear message:\n%s", toastText(*m))
	}
	c, _ := s.connection("evil")
	if _, err := dbAIPassword(root, c); err == nil {
		t.Error("the AI path resolved a project ${cmd:}")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the AI path ran the project's ${cmd:}")
	}
}

// ${dotenv:} reads the workspace .env only: never ../.env, never the
// process environment.
func TestDBDotenvReferenceNeverReadsParentEnv(t *testing.T) {
	_, m := refTestSetup(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "ws")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	const secret = "parent-env-secret-123456"
	if err := os.WriteFile(filepath.Join(parent, ".env"), []byte("SS5_PGPW="+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SS5_PGPW", "process-env-secret-123456")
	s := newDBState()
	s.root = root
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres", Host: "127.0.0.1", Port: 9, Source: dbclient.SourceProject, Password: "${dotenv:SS5_PGPW}"}}
	s.nodes["pg"] = dbNode{kind: dbNodeConnection, name: "pg", path: "pg", conn: "pg"}
	m.databases = s
	m.databaseOpen("pg", true)
	if !strings.Contains(toastText(*m), "Could not resolve database password") {
		t.Fatalf("a missing workspace .env must be an error, not the parent's or the process's value:\n%s", toastText(*m))
	}
	if _, err := dbAIPassword(root, s.conns[0]); err == nil {
		t.Fatal("the AI path read a value from outside the workspace .env")
	}
	// With the value in the workspace .env it resolves and is registered.
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SS5_PGPW=ws-env-secret-1234567\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pw, err := dbAIPassword(root, s.conns[0])
	if err != nil || pw != "ws-env-secret-1234567" {
		t.Fatalf("dbAIPassword = %q %v", pw, err)
	}
	if got := ai.Redact("x " + pw + " y"); strings.Contains(got, pw) {
		t.Fatalf("a resolved .env value is not redacted: %q", got)
	}
}

// A resolved reference value is registered, so it never reaches logs,
// toasts, the AI or errors.log.
func TestResolvedReferenceValueIsRedacted(t *testing.T) {
	_, m := refTestSetup(t)
	const resolved = "ss5-redact-me-value-123456"
	t.Setenv("SS5_REDACT", resolved)
	m.openAIKeyPrompt("openai")
	next, _ := m.update(prompt.SubmitMsg{Value: "${env:SS5_REDACT}"})
	*m = next
	_ = m.aiConfigured() // resolves through the store
	if got := ai.Redact("key is " + resolved); strings.Contains(got, resolved) {
		t.Fatalf("Redact leaves the resolved value: %q", got)
	}
	if got := ai.RedactSecrets("key is " + resolved); strings.Contains(got, resolved) {
		t.Fatalf("RedactSecrets leaves the resolved value: %q", got)
	}
}
