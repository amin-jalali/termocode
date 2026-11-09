package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The owner's "the AI's answers are dumb" chat: "how many tables do we
// have on remote?" got "there is no remote database" although
// BaaytonaDB goes through an SSH tunnel to the server, and the two
// container connections of the user file were never named. Then
// "BaaytonaDB" got "what would you like to inspect?" in place of the
// count. db_connections must say where each connection really points
// and whether it is remote; the guide must tell the model to act.

// remoteProjectConns is the owner's project file (fake data): one
// connection through an SSH tunnel, one local, one SQLite file (the
// project file holds no password: termocode refuses one there).
const remoteProjectConns = `{"connections":[` +
	`{"id":"BaaytonaDB","driver":"postgres","host":"127.0.0.1","port":5432,"user":"baaytona","database":"baaytona",` +
	`"ssh":{"host":"65.109.212.14","user":"deploy","auth":"agent"}},` +
	`{"id":"appdb","driver":"postgres","host":"localhost","port":5432,"database":"app"},` +
	`{"id":"notes","driver":"sqlite","path":"data/notes.db"}]}`

// remoteUserConns is the owner's user file (fake data): two database
// containers on the Baaytona docker context (ssh://deploy@…), made by
// Open in Databases, and a server by host name.
const remoteUserConns = `{"connections":[` +
	`{"id":"baaytona-db-1","driver":"postgres","host":"baaytona-db-1","port":5432,"user":"baaytona","database":"baaytona","password":"user-secret-2",` +
	`"ssh":{"host":"65.109.212.14","user":"deploy","auth":"agent"},"container":{"name":"baaytona-db-1","context":"Baaytona","engine":"docker"},"params":{"sslmode":"disable"}},` +
	`{"id":"rentcar-db-1","driver":"postgres","host":"rentcar-db-1","port":5432,"user":"rentcar","database":"rentcar",` +
	`"ssh":{"host":"65.109.212.14","user":"deploy","port":2222,"auth":"agent"},"container":{"name":"rentcar-db-1","context":"Baaytona"}},` +
	`{"id":"shop","driver":"mysql","host":"db.example.com","port":3306,"database":"shop"}]}`

// remoteSetup writes both files: the user file in a fake config home
// (XDG_CONFIG_HOME, set by the test to a temp dir), the project file in
// the workspace root. user "" skips the user file.
func remoteSetup(t *testing.T, root, user string) {
	t.Helper()
	if !strings.HasPrefix(os.Getenv("XDG_CONFIG_HOME"), os.TempDir()) {
		t.Fatal("XDG_CONFIG_HOME is not a temp dir: the test would touch the real config")
	}
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "db")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if user != "" {
		if err := os.WriteFile(filepath.Join(cfg, "connections.json"), []byte(user), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(remoteProjectConns), 0o644); err != nil {
		t.Fatal(err)
	}
}

func remoteLine(t *testing.T, out, id string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, id+" · ") {
			return l
		}
	}
	t.Fatalf("no line for %s:\n%s", id, out)
	return ""
}

// db_connections lists the project and the user file, each with where
// it really points (ssh tunnel, container on an ssh context, a host,
// local) and remote: yes / no; never a password.
func TestAIDBConnectionsSayWhereAndRemote(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	remoteSetup(t, root, remoteUserConns)
	tl, ok := ai.FindTool(aiDBTools(root, nil), "db_connections")
	if !ok {
		t.Fatal("no db_connections")
	}
	out, err := tl.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, wants := range map[string][]string{
		"BaaytonaDB":    {"postgres 127.0.0.1:5432/baaytona", "where: via ssh deploy@65.109.212.14", "remote: yes", "file: project"},
		"baaytona-db-1": {"where: container baaytona-db-1 on context Baaytona (ssh://deploy@65.109.212.14)", "remote: yes", "file: user"},
		"rentcar-db-1":  {"where: container rentcar-db-1 on context Baaytona (ssh://deploy@65.109.212.14:2222)", "remote: yes", "file: user"},
		"shop":          {"mysql db.example.com:3306/shop", "where: host db.example.com", "remote: yes", "file: user"},
		"appdb":         {"where: local", "remote: no", "file: project"},
		"notes":         {"sqlite data/notes.db", "where: local", "remote: no"},
	} {
		line := remoteLine(t, out, id)
		for _, w := range wants {
			if !strings.Contains(line, w) {
				t.Errorf("%s lacks %q: %s", id, w, line)
			}
		}
	}
	if !strings.Contains(out, "remote: yes = not on this computer") {
		t.Errorf("no word on what remote means:\n%s", out)
	}
	for _, secret := range []string{"user-secret-2"} {
		if strings.Contains(out, secret) {
			t.Errorf("a password in the list:\n%s", out)
		}
	}
}

// A user file that does not parse is named, not dropped in silence.
func TestAIDBConnectionsNameABrokenFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	remoteSetup(t, root, `{"connections":[{"id":"x",}`)
	tl, _ := ai.FindTool(aiDBTools(root, nil), "db_connections")
	out, err := tl.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "the user connection file could not be read") || !strings.Contains(out, "BaaytonaDB · ") {
		t.Errorf("broken user file:\n%s", out)
	}
}

// aiActRuleWants are the guide's "act, do not ask" rules.
var aiActRuleWants = []string{
	"Act, do not ask",
	"call it now",
	"the answer to your last question",
	"\"Remote\" means",
	"SSH tunnel",
	"docker context on ssh://",
	"never say you cannot reach a server before you tried",
	"Never give the owner a command to run in place of a tool",
	"same language as the owner",
}

// The identity (every step: a CLI step's instructions and the chat's
// own prompt) carries the rules.
func TestAIIdentityActsNotAsks(t *testing.T) {
	root := t.TempDir()
	for what, text := range map[string]string{
		"identity":        aiIdentityPrompt(root),
		"cli":             aiCLIInstructions(root, nil, false),
		"cli with tools":  aiCLIInstructions(root, []ai.Tool{{Name: "db_connections"}}, false),
		"chat with guide": aiWithToolGuide(aiIdentityPrompt(root), []ai.Tool{{Name: "db_query"}}, false),
	} {
		for _, w := range aiActRuleWants {
			if !strings.Contains(text, w) {
				t.Errorf("%s misses %q:\n%s", what, w, text)
			}
		}
	}
}

// Scripted: "how many tables do we have on remote?" The fake model reads
// the rules in its system prompt, lists the connections (BaaytonaDB and
// the containers show remote: yes) and goes straight to db_schema on
// BaaytonaDB, no card. Its tunnel is closed here, so the error says how
// to open it (db_connection op connect), not "ask the user".
func TestAIRemoteTablesScripted(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{
		toolCall("db_connections", nil),
		toolCall("db_schema", map[string]any{"conn": "BaaytonaDB"}),
		{Delta: "BaaytonaDB is on the server."},
	}}
	m, dir := aiIDETestModel(t, p)
	remoteSetup(t, dir, remoteUserConns)
	confirms := 0
	driveAIChat(t, m, m.sendAIChat("how many tables do we have on remote?"), func(aiChatConfirmMsg) string {
		confirms++
		return "deny"
	})
	if confirms != 0 {
		t.Errorf("%d cards for reads", confirms)
	}
	p.mu.Lock()
	sys := ""
	if len(p.seen) > 0 && len(p.seen[0]) > 0 {
		sys = p.seen[0][0].Content
	}
	p.mu.Unlock()
	for _, w := range aiActRuleWants {
		if !strings.Contains(sys, w) {
			t.Errorf("system prompt misses %q", w)
		}
	}
	res := strings.Join(p.toolResults(), "\n---\n")
	for _, w := range []string{
		"where: via ssh deploy@65.109.212.14 · remote: yes",
		"baaytona-db-1 · ", "container baaytona-db-1 on context Baaytona",
		"rentcar-db-1 · ",
		"db_connection", "op connect",
	} {
		if !strings.Contains(res, w) {
			t.Errorf("tool results lack %q:\n%s", w, res)
		}
	}
	if strings.Contains(res, "user-secret-2") {
		t.Errorf("a password reached the model:\n%s", res)
	}
}
