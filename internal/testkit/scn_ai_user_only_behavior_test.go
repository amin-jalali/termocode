package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// SCN-2: the AI can not do what only the user may do. Every case goes
// through the real tool-call path: a fake model asks for the calls, the
// real app (with nvim) runs them, and the test reads the tool results the
// model got back and the files, settings and git state on disk. The
// palette and settings cases are made from the policy tables, so a new
// command id or setting is covered the day it is added.

// scnCanary is text a test hides in places the AI must never read. It is
// built at run time, so no token-shaped literal sits in the source.
func scnCanary() string {
	return "scn" + "canary" + strings.Repeat("7", 3) + "x" + time.Now().Format("150405")
}

// scnRig is the workspace of the SCN-2 tests: a git repo, a folder next to
// it that the AI must never reach (with a secret file), a symlink in the
// workspace that points at that folder, and the user's private HTTP env
// file (it holds secrets).
type scnRig struct {
	d       *Driver
	model   *scnModel
	outside string
	ran     string // outside agents on PATH write here when started
	secret  string
	config  string
}

func newSCNRig(t *testing.T, extraConfig string, turns ...[]toolCall) *scnRig {
	t.Helper()
	r := &scnRig{secret: scnCanary()}
	model, srv := newSCNModel(t, turns...)
	r.model = model
	if testing.Short() {
		t.Skip("starts nvim")
	}
	cfg := `"ai_auto_approve":[]`
	if extraConfig != "" {
		cfg = extraConfig + "," + cfg
	}
	r.d = New(t, Options{W: 160, H: 40, Nvim: true, Git: true, Files: textFiles(),
		Config: map[string]string{"config.json": `{"animations":"off","images":"blocks","setup_done":true,` + cfg + `}`},
		Setup: func(t testing.TB, dir string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
			// Fake outside agents first on PATH: one that starts leaves a
			// mark, and the user's real ones are never run.
			bin := filepath.Join(filepath.Dir(dir), "bin")
			r.ran = filepath.Join(filepath.Dir(dir), "agent-ran")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, a := range []string{"claude", "codex", "gemini", "opencode", "aider"} {
				script := "#!/bin/sh\necho " + a + " >> '" + r.ran + "'\n"
				if err := os.WriteFile(filepath.Join(bin, a), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			r.outside = filepath.Join(filepath.Dir(dir), "outside")
			if err := os.MkdirAll(r.outside, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(r.outside, "secret.txt"), []byte("token="+r.secret+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(r.outside, filepath.Join(dir, "linked")); err != nil {
				t.Fatal(err)
			}
			env := `{"dev":{"token":"` + r.secret + `"}}`
			if err := os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), []byte(env), 0o600); err != nil {
				t.Fatal(err)
			}
		}})
	r.config = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode")
	open(r.d, "a.txt")
	return r
}

// snapshot is the workspace (with git), the outside folder and the
// user's config folder (settings, keys, profiles, permissions).
func (r *scnRig) snapshot(t *testing.T) scnSnapshot {
	t.Helper()
	// Left out: what the app itself keeps while it runs (the chat, the
	// session, the error log).
	return takeSCNSnapshot(t, r.d.Dir, map[string]string{"ws": r.d.Dir, "outside": r.outside, "config": r.config},
		".git", "ai/chats", "session.json", "sessions", "errors.log")
}

// scnCardTitles are the titles of the cards the AI's calls open.
var scnCardTitles = []string{"AI wants to", "Review AI changes"}

// scnCardLine is the line of a card that names what it asks. A confirm
// card can sit on top of the review (the review of a batch of writes opens
// first), so it wins.
func scnCardLine(s string) string {
	for _, c := range scnCardTitles {
		for _, l := range strings.Split(s, "\n") {
			if i := strings.Index(l, c); i >= 0 {
				return strings.TrimSpace(strings.Trim(l[i:], "│ "))
			}
		}
	}
	return ""
}

func scnCardOpen(d *Driver) bool {
	s := screen(d)
	for _, c := range scnCardTitles {
		if strings.Contains(s, c) {
			return true
		}
	}
	return false
}

// runTurn asks the AI and answers every card it opens with keys[i % len]
// (Esc or Enter: neither may ever say yes). It returns the title line of
// each card and fails when the turn does not end.
func (r *scnRig) runTurn(t *testing.T, keys ...string) []string {
	t.Helper()
	d := r.d
	askAI(d, "do it")
	var cards []string
	deadline := time.Now().Add(6 * time.Minute) // about a second per card under -race
	for !strings.Contains(screen(d), "fake-done") {
		if time.Now().After(deadline) {
			t.Fatalf("the turn did not end (%d cards answered):\n%s", len(cards), screen(d))
		}
		d.WaitFor(func() bool { return scnCardOpen(d) || strings.Contains(screen(d), "fake-done") }, 5*time.Second)
		if !scnCardOpen(d) {
			continue
		}
		before := screen(d)
		line := scnCardLine(before)
		cards = append(cards, line)
		if strings.HasPrefix(line, "Review AI changes") {
			clickNth(d, "Reject file", 0) // Esc only hides the review
		} else {
			k := keys[(len(cards)-1)%len(keys)]
			d.Key(k)
		}
		d.WaitFor(func() bool { return screen(d) != before }, 5*time.Second)
	}
	return cards
}

// scnNeverActions are the palette ids the AI may never run, from the
// policy table.
func scnNeverActions(t *testing.T) []app.AIPolicyDoc {
	t.Helper()
	var out []app.AIPolicyDoc
	for _, p := range app.AIActionPolicyCatalog() {
		if p.Gate == "never" {
			out = append(out, p)
		}
	}
	if len(out) < 100 {
		t.Fatalf("only %d never ids in the policy table", len(out))
	}
	return out
}

// scnNeverSettings are the settings the AI may never change.
func scnNeverSettings(t *testing.T) []app.AIPolicyDoc {
	t.Helper()
	var out []app.AIPolicyDoc
	for _, p := range app.AISettingPolicyCatalog() {
		if p.Gate == "never" {
			out = append(out, p)
		}
	}
	if len(out) < 10 {
		t.Fatalf("only %d never settings in the policy table", len(out))
	}
	return out
}

// scnExtra is one hand-made user-only call: what it tries, and a part of
// the refusal the model must get.
type scnExtra struct {
	what string
	call toolCall
	want string
}

func scnExtras(r *scnRig) []scnExtra {
	outsideSecret := filepath.Join(r.outside, "secret.txt")
	return []scnExtra{
		{"read a file outside the workspace", toolCall{"read_file", map[string]any{"path": "../outside/secret.txt"}}, "outside"},
		{"read a file outside by absolute path", toolCall{"read_file", map[string]any{"path": outsideSecret}}, "outside"},
		{"read the user's config", toolCall{"read_file", map[string]any{"path": filepath.Join(r.config, "config.json")}}, "outside"},
		{"read through a symlink that leaves the workspace", toolCall{"read_file", map[string]any{"path": "linked/secret.txt"}}, "outside"},
		{"list a folder through a symlink that leaves the workspace", toolCall{"list_dir", map[string]any{"path": "linked"}}, "outside"},
		{"read the private HTTP env file", toolCall{"read_file", map[string]any{"path": "http-client.private.env.json"}}, "secret"},
		{"search finds a secret value", toolCall{"search", map[string]any{"query": r.secret}}, "no matches"},
		{"write through a symlink that leaves the workspace", toolCall{"write_file", map[string]any{"path": "linked/new.txt", "content": "x"}}, "outside"},
		{"create a file outside", toolCall{"create_file", map[string]any{"path": "../outside/new.txt", "content": "x"}}, "outside"},
		{"delete a file outside", toolCall{"delete", map[string]any{"path": "../outside/secret.txt"}}, "outside"},
		{"delete a file outside by absolute path", toolCall{"delete", map[string]any{"path": outsideSecret}}, "outside"},
		{"delete through a symlink that leaves the workspace", toolCall{"delete", map[string]any{"path": "linked/secret.txt"}}, "outside"},
		{"move a file out of the workspace", toolCall{"rename", map[string]any{"from": "a.txt", "to": "../outside/a.txt"}}, "outside"},
		{"set an AI key as a setting", toolCall{"settings_set", map[string]any{"key": "ai_api_key", "value": r.secret}}, "not for the AI"},
		{"turn on auto-approve for everything", toolCall{"settings_set", map[string]any{"key": "ai_auto_approve", "value": "run,git,fileWrite"}}, "not for the AI"},
		{"turn plan approval off", toolCall{"settings_set", map[string]any{"key": "ai_plan_approval", "value": "off"}}, "not for the AI"},
		{"run an unknown command id", toolCall{"execute_action", map[string]any{"id": "secrets-reveal"}}, "not an IDE command id"},
	}
}

// Spec (SCN-2): every palette command marked never, every setting marked
// never and every hand-made user-only call (secrets, files outside the
// workspace, auto-approve, plan approval …) is refused at once: no card,
// a refusal for the model, and nothing changes on disk, in git or in the
// app (it does not quit).
func TestSCNAIUserOnlyActionsRefused(t *testing.T) {
	var actions, settings []toolCall
	nevers := scnNeverActions(t)
	for _, p := range nevers {
		actions = append(actions, toolCall{"execute_action", map[string]any{"id": p.ID}})
	}
	nset := scnNeverSettings(t)
	for _, p := range nset {
		settings = append(settings, toolCall{"settings_set", map[string]any{"key": p.ID, "value": "on"}})
	}
	// The extras need the rig's paths; the model reads its turns lazily,
	// so fill the third turn in once the rig exists.
	turns := [][]toolCall{actions, settings, nil}
	r := newSCNRig(t, "", turns...)
	extras := scnExtras(r)
	r.model.mu.Lock()
	for _, e := range extras {
		r.model.turns[2] = append(r.model.turns[2], e.call)
	}
	r.model.mu.Unlock()
	before := r.snapshot(t)

	if cards := r.runTurn(t, "esc"); len(cards) != 0 {
		t.Errorf("%d cards opened where none may:\n%s", len(cards), strings.Join(cards, "\n"))
	}

	for j, p := range nevers {
		if got := r.model.result(0, j); !strings.Contains(got, "not allowed for the AI") {
			t.Errorf("execute_action %s: result %q, want a refusal", p.ID, got)
		}
	}
	for j, p := range nset {
		if got := r.model.result(1, j); !strings.Contains(got, "not for the AI") {
			t.Errorf("settings_set %s: result %q, want a refusal", p.ID, got)
		}
	}
	for j, e := range extras {
		got := r.model.result(2, j)
		if !strings.Contains(got, e.want) {
			t.Errorf("%s: result %q, want it to say %q", e.what, got, e.want)
		}
	}
	for id, res := range r.model.results() {
		if strings.Contains(res, r.secret) {
			t.Errorf("the secret reached the model in the result of %s", id)
		}
	}
	if r.d.Quit() {
		t.Error("the app quit")
	}
	if diff := before.diff(r.snapshot(t)); len(diff) > 0 {
		t.Errorf("refused calls changed things:\n%s", strings.Join(diff, "\n"))
	}
}

// scnConfirmActions are the palette ids that ask first, from the policy
// table.
func scnConfirmActions(t *testing.T) []app.AIPolicyDoc {
	t.Helper()
	var out []app.AIPolicyDoc
	for _, p := range app.AIActionPolicyCatalog() {
		if p.Gate == "confirm" {
			out = append(out, p)
		}
	}
	if len(out) < 50 {
		t.Fatalf("only %d confirm ids in the policy table", len(out))
	}
	return out
}

// scnAskingCalls are typed tools that must ask: git writes, commands, a
// raw shell line, file changes, a setting, an agent run, an outside agent.
var scnAskingCalls = []toolCall{
	{"git_push", map[string]any{}},
	{"git_commit", map[string]any{"message": "ai commit"}},
	{"git_checkout", map[string]any{"branch": "ai-branch", "create": true}},
	{"run_command", map[string]any{"command": "touch ran-by-ai.txt"}},
	{"terminal_send", map[string]any{"text": "touch typed-by-ai.txt"}},
	{"delete", map[string]any{"path": "b.txt"}},
	{"rename", map[string]any{"from": "b.txt", "to": "moved.txt"}},
	{"write_file", map[string]any{"path": "c.txt", "content": "changed by ai\n"}},
	{"create_dir", map[string]any{"path": "made-by-ai"}},
	{"settings_set", map[string]any{"key": "tab_size", "value": "8"}},
	{"agent_run_start", map[string]any{"task": "do more", "steps": []any{map[string]any{"title": "step", "prompt": "go"}}}},
	{"outside_agent_start", map[string]any{"agent": "claude"}},
}

// Spec (SCN-2): every palette command marked confirm and every asking
// tool shows its card; Esc and Enter (the focused Deny) both refuse, the
// model is told so, and nothing changes: files, git, settings, terminal
// tabs, and the app does not quit.
func TestSCNAIConfirmActionsDenyDoesNothing(t *testing.T) {
	confirms := scnConfirmActions(t)
	var actions []toolCall
	for _, p := range confirms {
		actions = append(actions, toolCall{"execute_action", map[string]any{"id": p.ID}})
	}
	r := newSCNRig(t, "", actions, scnAskingCalls)
	tabsBefore := len(termTabs(r.d))
	before := r.snapshot(t)

	cards := r.runTurn(t, "esc", "enter")

	declined := 0
	for j, p := range confirms {
		got := r.model.result(0, j)
		switch {
		case strings.Contains(got, "the user declined"):
			declined++
		case strings.HasPrefix(got, "error:"):
			t.Logf("execute_action %s refused before a card: %s", p.ID, got)
		default:
			t.Errorf("execute_action %s: result %q, want declined", p.ID, got)
		}
	}
	if declined < len(confirms)*3/4 {
		t.Errorf("only %d of %d confirm ids reached a card", declined, len(confirms))
	}
	for j, c := range scnAskingCalls {
		got := r.model.result(1, j)
		if !strings.Contains(got, "declined") && !strings.Contains(got, "rejected") {
			t.Errorf("%s: result %q, want declined", c.name, got)
		}
	}
	if len(cards) < declined+len(scnAskingCalls) {
		t.Errorf("%d cards for %d declined calls", len(cards), declined+len(scnAskingCalls))
	}
	if r.d.Quit() {
		t.Error("the app quit")
	}
	if b, err := os.ReadFile(r.ran); err == nil {
		t.Errorf("outside agents started: %s", b)
	}
	if n := len(termTabs(r.d)); n != tabsBefore {
		t.Errorf("terminal tabs %d → %d", tabsBefore, n)
	}
	if diff := before.diff(r.snapshot(t)); len(diff) > 0 {
		t.Errorf("denied calls changed things:\n%s", strings.Join(diff, "\n"))
	}
}
