package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (owner, 2026-11-09): "We built the tools. termocode knows the
// containers, knows how to connect, knows the databases. So when the
// agent does something it must use our MCP tools, not go digging around
// on its own." For "how many tables in my database?" the AI listed every
// connection, connected two, ran psql itself and answered from one the
// owner had not set up. One call must be enough: the db_* tools without
// conn use the project's own connection and say so, db_connections puts
// it first, and a connection termocode made from Containers is never
// taken for the owner's own.

const aiOneProjectConn = `{"connections":[{"id":"baaytonadb","name":"BaaytonaDB","driver":"postgres","host":"localhost","port":5432,"user":"app","database":"app"}]}`

const aiContainerMadeConn = `{"connections":[{"id":"baaytona-db-1","driver":"postgres","host":"baaytona-db-1","port":5432,"user":"app","database":"app",` +
	`"ssh":{"host":"srv.example","user":"me"},"container":{"name":"baaytona-db-1","context":"srv","engine":"docker"}}]}`

func aiWriteConns(t *testing.T, e *aiDBEnv, project, user string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, ".termocode", "databases.json"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	if user == "" {
		return
	}
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "db", "connections.json"), []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAIDBWithoutConnUsesTheProjectsConnection(t *testing.T) {
	e := newAIDBEnv(t)
	aiWriteConns(t, e, aiOneProjectConn, aiContainerMadeConn)
	for _, name := range []string{"db_schema", "db_query"} {
		for _, r := range e.tool(t, name).Required {
			if r == "conn" {
				t.Errorf("%s still requires conn", name)
			}
		}
	}
	if _, _, err := e.gate(t, "db_schema", map[string]any{}); err != nil {
		t.Fatalf("db_schema without conn refused: %v", err)
	}
	out, err := e.run(t, "db_schema", map[string]any{"schema": "public"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "baaytonadb") || !strings.Contains(out, "project") || !strings.Contains(out, "users") {
		t.Errorf("db_schema without conn does not say it used the project's connection:\n%s", out)
	}
	if len(e.conns) != 1 || e.conns[0].ID != "baaytonadb" {
		t.Fatalf("opened %+v, want only baaytonadb", e.conns)
	}
	out, err = e.run(t, "db_query", map[string]any{"sql": "SELECT 42"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "baaytonadb") {
		t.Errorf("db_query without conn does not name the connection:\n%s", out)
	}
	for _, c := range e.conns {
		if c.ID != "baaytonadb" {
			t.Errorf("opened %s: only the project's connection may be used", c.ID)
		}
	}
}

// Several project connections (or none): one line with the ids and the
// question which one; nothing is opened.
func TestAIDBWithoutConnAsksWhenNotOne(t *testing.T) {
	e := newAIDBEnv(t) // three project connections
	_, _, err := e.gate(t, "db_schema", map[string]any{})
	if err == nil {
		t.Fatal("db_schema without conn ran with three project connections")
	}
	msg := err.Error()
	if strings.Contains(msg, "\n") || !strings.Contains(msg, "local, prod, ro") || !strings.Contains(msg, "which") {
		t.Errorf("want one line listing local, prod, ro and asking which: %q", msg)
	}
	if len(e.opened) != 0 {
		t.Fatalf("opened %d sessions", len(e.opened))
	}
}

func TestAIDBConnectionsProjectFirstAndContainerMadeMarked(t *testing.T) {
	e := newAIDBEnv(t)
	aiWriteConns(t, e, aiOneProjectConn, aiContainerMadeConn)
	out, err := e.run(t, "db_connections", nil)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "baaytonadb ") || strings.HasPrefix(l, "baaytona-db-1 ") {
			rows = append(rows, l)
		}
	}
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "baaytonadb ") {
		t.Fatalf("the project's connection is not first:\n%s", out)
	}
	if !strings.Contains(rows[0], "project (default)") {
		t.Errorf("project row not marked default: %q", rows[0])
	}
	if !strings.Contains(rows[1], "added by termocode from Containers") {
		t.Errorf("container-made row not marked: %q", rows[1])
	}
}

// The instructions every step gets: databases, containers, servers,
// kubernetes and git go through termocode's tools only.
func TestAIInstructionsSayOnlyOurTools(t *testing.T) {
	for _, text := range []string{aiIdentityPrompt("/p"), aiCLIInstructions("/p", nil, false), aiCLIInstructions("/p", nil, true)} {
		for _, want := range []string{"databases, containers, servers, kubernetes and git go through termocode's tools",
			"never run psql, docker, ssh, kubectl or git yourself", "never explore other connections or routes", "report its error",
			// "or if it doesn't exist, help the user build it"
			"help the owner build it", "db_connection op add or edit", "contexts_manage add",
			"never edit config files by hand", "never invent a value"} {
			if !strings.Contains(text, want) {
				t.Errorf("instructions miss %q", want)
			}
		}
	}
}

// Spec (owner, 2026-11-09): "or if it doesn't exist, help the user build
// it — the deal was everything is done with AI's help". "how many tables
// in my db?" with no connection: the AI reads what termocode knows
// (contexts_list, containers_list: the container's POSTGRES_USER /
// POSTGRES_DB and its published port, never a secret), proposes the
// connection with db_connection op add prefilled from that, the owner
// approves the exact card, and db_schema answers from the new one.
func TestAIBuildsTheMissingConnectionWithOurTools(t *testing.T) {
	e := newAIDBEnv(t)
	aiWriteConns(t, e, `{"connections":[]}`, "")
	e.m.databases = newDBState()
	e.m.databases.load(e.root)
	var read []string
	fake := func(name, out string) ai.Tool {
		return ai.Tool{Name: name, ReadOnly: true, Run: func(context.Context, map[string]any) (string, error) {
			read = append(read, name)
			return out, nil
		}}
	}
	tools := append([]ai.Tool{
		fake("contexts_list", "default (current) · unix:///var/run/docker.sock · local"),
		fake("containers_list", "engine docker · context default\napp-db-1 · postgres:16 · running · 0.0.0.0:5432->5432/tcp · env POSTGRES_USER=app POSTGRES_DB=app"),
		{Name: "run_command", Mutating: true, Class: ai.ClassRun, Run: func(context.Context, map[string]any) (string, error) {
			t.Error("run_command ran")
			return "", nil
		}},
	}, e.tools...)
	add := map[string]any{"op": "add", "id": "app-db", "driver": "postgres", "host": "localhost", "port": 5432, "user": "app", "database": "app"}
	p := &scriptedAIProvider{replies: []ai.Chunk{
		toolCall("db_schema", map[string]any{"schema": "public"}),
		toolCall("run_command", map[string]any{"command": "docker ps"}),
		toolCall("contexts_list", nil),
		toolCall("containers_list", nil),
		toolCall("db_connection", add),
		toolCall("db_schema", map[string]any{"schema": "public"}),
		{Delta: "app-db has 2 tables and views: users, active."},
	}}
	var cards []string
	agent := &ai.Agent{Provider: p, Tools: tools, Native: true,
		Confirm: func(_ context.Context, tl ai.Tool, human string) bool {
			cards = append(cards, tl.Name+": "+human)
			return true
		}}
	agent.Run(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: "how many tables in my db?"}}, func(ai.Event) {})
	res := p.toolResults()
	if len(res) != 6 {
		t.Fatalf("tool results %d: %q", len(res), res)
	}
	if !strings.Contains(res[0], "db_connection") || !strings.Contains(res[0], "add") {
		t.Errorf("no connection: the error does not point to building one with db_connection op add: %q", res[0])
	}
	if !strings.Contains(res[1], "containers_list") {
		t.Errorf("docker ps was not refused with the tool to use: %q", res[1])
	}
	if strings.Join(read, ",") != "contexts_list,containers_list" {
		t.Errorf("read %v", read)
	}
	if len(cards) != 1 || !strings.HasPrefix(cards[0], "db_connection: ") || !strings.Contains(cards[0], "localhost:5432") {
		t.Fatalf("want one db_connection card with the prefilled target, got %q", cards)
	}
	if !strings.Contains(res[4], "added app-db") {
		t.Errorf("add: %q", res[4])
	}
	if !strings.Contains(res[5], "app-db") || !strings.Contains(res[5], "users") {
		t.Errorf("db_schema did not answer from the new connection: %q", res[5])
	}
}
