package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// Spec (MS-7): a connection whose password sits in the locked secret
// store gives the agent a clear error that says to unlock in termocode.
// The agent never gets a passphrase prompt, no prompt opens in the UI,
// and no session is opened.
func TestMCPServeDBLockedStoreClearError(t *testing.T) {
	e := newMCPServeDBEnv(t)
	dir, _ := withSecretSetting(t, "age", false)
	s, err := secrets.Open(secrets.Options{Dir: dir, Backend: "age", ScryptWorkFactor: 14})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("db.local", "db-pass-123456"); err != nil {
		t.Fatal(err)
	}
	s.Lock()
	if !storeLocked(t) {
		t.Fatal("store should start locked")
	}
	e.model.secrets = &secretsState{}
	r, err := e.call(t, "codex", "db_query", map[string]any{"conn": "local", "sql": "SELECT 1"})
	text := mcpServeText(r)
	if err != nil || !r.IsError || !strings.Contains(text, "secret store is locked") || !strings.Contains(text, "unlock") {
		t.Fatalf("locked store: %+v %v", r, err)
	}
	if strings.Contains(strings.ToLower(text), "passphrase:") || strings.Contains(text, "db-pass-123456") {
		t.Fatalf("the agent was asked for, or got, a secret: %q", text)
	}
	if e.model.secretsPromptOpen() {
		t.Fatal("a passphrase prompt opened for an outside agent's call")
	}
	if len(e.opened) != 0 {
		t.Fatalf("%d sessions opened without the password", len(e.opened))
	}
	if !storeLocked(t) {
		t.Fatal("the agent's call unlocked the store")
	}
}

// Spec (MS-7): the card shows the connection and the SQL, with registered
// secrets hidden; Deny is -32000 and no session is opened.
func TestMCPServeDBCardShowsRedactedSQL(t *testing.T) {
	e := newMCPServeDBEnv(t)
	const token = "tok-mcp-db-card-5551234"
	ai.RegisterSecret(token)
	m := &e.model
	m.output = newOutputStore()
	m.mcpServe = &mcpServeState{hub: e.hub}
	src := newMCPServeSource(e.hub, e.root)
	call := mcpServeStartCall(src, "codex", "db_query", map[string]any{"conn": "local", "sql": "SELECT id FROM users WHERE token = '" + token + "'"})
	mcpServePumpUntil(t, m, e.hub, "db_query card", func() bool { return mcpServeCardOpen(m, "db_query") })
	view := m.confirm.View()
	for _, want := range []string{"codex", "Read on local", "SELECT id FROM users WHERE token"} {
		if !strings.Contains(view, want) {
			t.Errorf("card lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, token) {
		t.Fatalf("the card shows a registered secret:\n%s", view)
	}
	mcpServeAnswer(m, "deny")
	got := mcpServeAwait(t, m, e.hub, call)
	var ce *mcp.CallError
	if !errors.As(got.err, &ce) || ce.Code != mcp.CodeDeclined || len(e.opened) != 0 {
		t.Fatalf("deny: %+v, %d sessions", got, len(e.opened))
	}
}

// Spec (MS-7): an outside agent gets exactly the in-app chat's text for
// the same read (same caps, same redaction): the connection password and
// a registered secret are hidden, long cells are cut, db_ai_max_kb caps
// the answer.
func TestMCPServeDBResultParityWithChat(t *testing.T) {
	e := newMCPServeDBEnv(t)
	const pw = "pw-mcp-parity-778899"
	const token = "tok-mcp-parity-secret-4242"
	ai.RegisterSecret(token)
	if err := os.MkdirAll(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode"), 0o700); err != nil {
		t.Fatal(err)
	}
	p, _ := settingsConfigPath()
	if err := os.WriteFile(p, []byte(`{"db_ai_max_kb": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e.result = func(sql string) dbclient.Result {
		r := dbclient.Result{Columns: []dbclient.Column{{Name: "k"}, {Name: "v"}}}
		r.Rows = append(r.Rows, []dbclient.Cell{{Text: "password"}, {Text: pw}}, []dbclient.Cell{{Text: "token"}, {Text: "Bearer " + token}})
		for i := 0; i < 200; i++ {
			r.Rows = append(r.Rows, []dbclient.Cell{{Text: fmt.Sprint(i)}, {Text: strings.Repeat("x", 200)}})
		}
		return r
	}
	// Both sides know the password the view holds (the user connected).
	e.aiDBEnv.m.databases.passwords["local"] = pw
	e.model.databases = newDBState()
	e.model.databases.load(e.root)
	e.model.databases.passwords["local"] = pw
	args := map[string]any{"conn": "local", "sql": "SELECT k, v FROM settings", "max_rows": 5000}
	chat, err := e.aiDBEnv.tool(t, "db_query").Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.call(t, "codex", "db_query", args)
	if err != nil || r.IsError {
		t.Fatalf("mcp: %+v %v", r, err)
	}
	got := mcpServeText(r)
	if got != chat {
		t.Fatalf("outside agent text differs from the chat's\nmcp:  %q\nchat: %q", got, chat)
	}
	if strings.Contains(got, pw) || strings.Contains(got, token) || !strings.Contains(got, "[hidden]") {
		t.Fatalf("a secret reached the agent:\n%s", got)
	}
	if len(got) > 1200 || !strings.Contains(got, "[truncated;") || strings.Contains(got, strings.Repeat("x", 81)) {
		t.Fatalf("not capped like the chat (%d bytes)", len(got))
	}
	if strings.Contains(e.audit.all(), pw) || strings.Contains(e.audit.all(), token) {
		t.Fatal("the audit line holds a secret")
	}
}
