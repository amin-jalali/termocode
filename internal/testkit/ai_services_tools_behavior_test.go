package testkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The plan's §4.4 flow, end to end with a fake model and the fake docker:
// "my app can't connect to the database — why?"
//
// The model lists the services, reads api's status, the postgres logs and
// .env, then restarts api. The restart asks; the user picks "Always in
// this project"; a second restart in the same project does not ask. The
// postgres password never reaches the model or the screen.
func TestAIServicesDatabaseFlow(t *testing.T) {
	f := svcFake(t)
	f.Set(t, "logs-postgres.txt", strings.Join([]string{
		`postgres-1  | 2026-10-16T11:03:55Z LOG:  database system is ready to accept connections`,
		`postgres-1  | 2026-10-16T11:03:55Z DETAIL: connection DATABASE_URL=postgres://app:hunter2hunter2@localhost:5432/app`,
		`postgres-1  | 2026-10-16T11:03:55Z FATAL:  password authentication failed for user "app"`,
	}, "\n")+"\n")
	f.Set(t, "logs-api.txt", "api-1  | dial tcp 127.0.0.1:5432: connect: connection refused\n")
	s, srv := fakeToolModel(t,
		toolCall{"services_list", map[string]any{}},
		toolCall{"services_status", map[string]any{"name": "api"}},
		toolCall{"services_logs", map[string]any{"name": "api", "lines": 60}},
		toolCall{"services_logs", map[string]any{"name": "postgres", "lines": 40}},
		toolCall{"read_file", map[string]any{"path": ".env"}},
		toolCall{"services_restart", map[string]any{"name": "api"}},
		toolCall{"services_restart", map[string]any{"name": "api"}},
		toolCall{"services_start", map[string]any{"name": "nope"}},
	)
	files := textFiles()
	files["compose.yaml"] = svcCompose
	files[".env"] = "POSTGRES_PASSWORD=hunter2hunter2\nPOSTGRES_USER=postgres\nDATABASE_URL=postgres://app:hunter2hunter2@localhost:5432/app\n"
	d := aiDriver(t, srv.URL, files, false)
	open(d, "a.txt")
	askAI(d, "my app can't connect to the database — why?")

	if !d.WaitForText("AI wants to", 15*time.Second) {
		t.Fatalf("no confirm card for services_restart:\n%s", screen(d))
	}
	sc := screen(d)
	for _, want := range []string{"services_restart", "Restart service api (docker compose restart api)",
		"Allow", "Always allow services", "Always in project", "Deny"} {
		if !strings.Contains(sc, want) {
			t.Errorf("confirm card has no %q:\n%s", want, sc)
		}
	}
	clickTextOn(d, "Always in project")
	if !d.WaitForText("fake-done", 20*time.Second) {
		t.Fatalf("no final answer (a dialog may be waiting):\n%s", screen(d))
	}
	if n := f.Count("restart api"); n != 2 {
		t.Errorf("restart api ran %d times, want 2 (the second without a card): %q", n, f.Calls())
	}
	r := s.toolResults()
	if len(r) != 8 {
		t.Fatalf("%d results: %q", len(r), r)
	}
	all := strings.Join(r, "\n=====\n")
	if strings.Contains(all, "hunter2hunter2") || strings.Contains(screen(d), "hunter2hunter2") {
		t.Errorf("the password reached the model or the screen:\n%s", all)
	}
	checks := []struct {
		i    int
		want []string
	}{
		{0, []string{`project "termocode"`, "postgres [compose] image postgres:16", "ports 5432→5432/tcp", "healthcheck yes",
			"env keys POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER", "depends_on postgres", "last state: exited (1)", "compose.yaml:"}},
		{1, []string{"api [compose]: exited", "exit code 1", "Exited (1) 5 minutes ago"}},
		{2, []string{"[1 lines,", "connection refused"}},
		{3, []string{"redacted values]", `password authentication failed for user "app"`, "postgres://app:[REDACTED]@localhost:5432/app"}},
		{4, []string{"POSTGRES_PASSWORD=[REDACTED]"}},
		{5, []string{"exited (1) 2 s after restart", "services_logs"}},
		{7, []string{`unknown service "nope"`, "postgres"}},
	}
	for _, c := range checks {
		for _, w := range c.want {
			if !strings.Contains(r[c.i], w) {
				t.Errorf("result %d has no %q:\n%s", c.i, w, r[c.i])
			}
		}
	}
	// The project answer lives in the session file, not in the repo.
	if _, err := os.Stat(filepath.Join(d.Dir, ".termocode")); err == nil {
		t.Error("trust must not be written into the project")
	}
	sess := readWorkspaceSession(t, d)
	if v, _ := sess["services_ai_allowed"].(bool); !v {
		t.Errorf("services_ai_allowed not saved: %v", sess)
	}
}

// readWorkspaceSession reads sessions/<hash>.json of the workspace.
func readWorkspaceSession(t *testing.T, d *Driver) map[string]any {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "sessions")
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if ws, _ := m["workspace"].(string); ws != "" && strings.HasSuffix(ws, filepath.Base(d.Dir)) {
				return m
			}
		}
	}
	t.Fatalf("no session file in %s", dir)
	return nil
}
