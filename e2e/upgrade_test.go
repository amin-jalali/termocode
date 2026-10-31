//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// SCN-6: a HOME written by an older termocode, started with this one.
//
// testdata/upgrade/<version>/ is the config and state an older build
// wrote (a real run of that commit: settings and keys written by hand,
// then a few files opened, a palette command, one AI chat against a fake
// model, and unsaved text left by a terminal hang-up). Paths are
// placeholders: {{HOME}}, {{WS}}, {{AI_URL}}; WSKEY is the workspace key
// and PID the process that left the hot-exit backup.
var upgradeVersions = []string{
	"v2026-10-15-release", // the first release-ready state (6393016c)
	"v2026-10-19",         // ten days later: profiles, MCP, secrets index (e39c5307)
}

const (
	oldAIKey      = "sk-old-FAKE-0001"
	oldUnsaved    = "old-unsaved-marker-42"
	oldChatAsk    = "hello from the old version"
	oldChatAnswer = "old answer from the fake model"
)

// fakeOldModel is an OpenAI-style chat server; it records the keys it
// was called with.
type fakeOldModel struct {
	srv  *httptest.Server
	mu   sync.Mutex
	auth []string
}

func newFakeOldModel(t *testing.T, answer string) *fakeOldModel {
	t.Helper()
	f := &fakeOldModel{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		content, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answer}}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", content)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOldModel) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auth...)
}

// deadPID is the pid of a process that has ended.
func deadPID(t *testing.T) string {
	t.Helper()
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return strconv.Itoa(c.ProcessState.Pid())
}

// installOldHome copies testdata/upgrade/<version> into home, filling in
// the placeholders. It returns the files it wrote (relative to home).
func installOldHome(t *testing.T, version, home, ws, aiURL string) []string {
	t.Helper()
	src := filepath.Join("testdata", "upgrade", version)
	pid := deadPID(t)
	r := strings.NewReplacer("{{HOME}}", home, "{{WS}}", ws, "{{AI_URL}}", aiURL)
	var wrote []string
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		rel = strings.ReplaceAll(rel, "WSKEY", hashKey(ws))
		rel = strings.ReplaceAll(rel, string(filepath.Separator)+"PID"+string(filepath.Separator), string(filepath.Separator)+pid+string(filepath.Separator))
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		writeFile(t, filepath.Join(home, rel), r.Replace(string(b)))
		wrote = append(wrote, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return wrote
}

// readJSON reads a JSON object file.
func readJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	var m map[string]any
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse %s: %v\n%s", p, err, b)
	}
	return m
}

// Spec (SCN-6): the current binary starts on an older HOME and keeps
// everything: the settings and their values, the key overrides, the
// session (tabs) and the unsaved text of the hot-exit backup, the saved
// AI chats and profiles, and the AI key, which moves from the plain file
// to the encrypted store with the passphrase flow (the plain file goes on
// the Delete answer of the card after the move) and still works. No
// file the old version wrote is lost.
func TestUpgradeFromOldHome(t *testing.T) {
	for _, version := range upgradeVersions {
		t.Run(version, func(t *testing.T) { testUpgradeFrom(t, version) })
	}
}

func testUpgradeFrom(t *testing.T, version string) {
	model := newFakeOldModel(t, "new answer after the upgrade")
	home, ws := t.TempDir(), NewRepo(t)
	old := installOldHome(t, version, home, ws, model.srv.URL)
	before := map[string]string{}
	for _, rel := range old {
		before[rel] = readFileOrEmpty(filepath.Join(home, rel))
	}
	cfg := filepath.Join(home, ".config", "termocode")
	plain := filepath.Join(cfg, "ai", "credentials.json")

	tm := Start(t, StartOpts{Home: home, Workspace: ws, Cols: 160, Rows: 44})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})

	// The session: the old tabs are back.
	tm.WaitFor("the old tabs", uiTimeout, func(ls []string) bool {
		return strings.Contains(ls[0], "README.md") && strings.Contains(ls[0], "main.go") && strings.Contains(ls[0], "notes.txt")
	})
	// The old AI settings are used.
	tm.WaitText("gpt-old-model", uiTimeout)

	// The plain key file: the move card, the encrypted file (first
	// button), a new passphrase twice.
	tm.WaitText("Move your secrets to a safer store?", bootTimeout)
	tm.WaitText("1 AI key", uiTimeout)
	tm.noSecretOnScreen(oldAIKey)
	tm.settle()
	tm.Send(KeyEnter)
	tm.typeMasked(secretsPrompt, secretsPass)
	tm.typeMasked("Type it again:", secretsPass)
	tm.WaitText("Secrets moved", secretsWait)

	// The hot-exit backup the old version left.
	tm.WaitText("Restore unsaved changes?", uiTimeout)
	tm.settle()
	tm.ClickText("Restore ")
	tm.WaitText(oldUnsaved, uiTimeout)
	tm.WaitFor("notes.txt tab, modified", uiTimeout, func(ls []string) bool {
		return strings.Contains(ls[0], "notes.txt ●")
	})
	tm.step("restored")

	// The card that waited behind it asks before the plain key file goes
	// (Keep for now is first); the user picks Delete the plain file.
	tm.WaitText("Delete the old plain file?", uiTimeout)
	tm.settle()
	tm.Send("\x1b[C") // Right: Delete the plain file
	tm.settle()
	tm.Send(KeyEnter)
	waitFile(t, plain, false, "Delete the plain file left credentials.json")

	// keymap.json: F6 is the command palette (its default is another
	// action).
	tm.Send("\x1b[17~")
	tm.WaitText("Command Palette", uiTimeout)
	tm.Send(KeyEsc)
	tm.WaitGone("Command Palette", uiTimeout)

	// The old chat is the one shown, and a new turn uses the moved key.
	tm.Send("\x1ba")
	tm.WaitText("AI CHAT", uiTimeout)
	tm.WaitText(oldChatAnswer, uiTimeout)
	tm.WaitText(oldChatAsk, uiTimeout)
	tm.settle()
	tm.Type("still there?")
	tm.Send(KeyEnter)
	tm.WaitText("new answer after the upgrade", secretsWait)
	if got := model.keys(); len(got) == 0 || got[len(got)-1] != "Bearer "+oldAIKey {
		t.Errorf("the model was called with %q, want the old key", got)
	}
	tm.Send("\x1ba")
	tm.WaitGone("AI CHAT", uiTimeout)

	if _, ok := before[".config/termocode/ai/profiles.json"]; ok {
		tm.Palette("AI: Switch Profile")
		tm.WaitText("old-reviewer", uiTimeout)
		tm.Send(KeyEsc)
		tm.WaitGone("old-reviewer", uiTimeout)
	}
	tm.quitDiscard()

	// Nothing the old version wrote is lost.
	// Files the user wrote stay byte for byte; state files (recents,
	// session, chats, logs) may be updated.
	userFiles := map[string]bool{
		".config/termocode/keymap.json":      true,
		".config/termocode/ai/profiles.json": true,
		".config/termocode/mcp.json":         true,
	}
	for rel, was := range before {
		p := filepath.Join(home, rel)
		switch {
		case strings.Contains(rel, "/backup/"):
			continue // the restored hot-exit backup is done with
		case p == plain:
			continue // moved; checked below
		}
		now, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s is gone: %v", rel, err)
			continue
		}
		if userFiles[rel] && string(now) != was {
			t.Errorf("%s changed:\n%s", rel, now)
		}
	}
	if _, err := os.Stat(plain + ".migrated"); err != nil {
		t.Errorf("no credentials.json.migrated after the move: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(cfg, "secrets.age")); err != nil || strings.Contains(string(b), oldAIKey) {
		t.Errorf("secrets.age missing or holds the key in clear (err %v)", err)
	}

	// Settings keep their values, unknown keys included.
	var want map[string]any
	if err := json.Unmarshal([]byte(before[".config/termocode/config.json"]), &want); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, filepath.Join(cfg, "config.json"))
	for k, v := range want {
		if fmt.Sprint(got[k]) != fmt.Sprint(v) {
			t.Errorf("config.json %s = %v, want %v", k, got[k], v)
		}
	}
	// Recent files, recent workspaces and the workspace session still
	// name the old files.
	for _, f := range []string{"recents.json", "sessions/" + hashKey(ws) + ".json"} {
		b := readFileOrEmpty(filepath.Join(cfg, f))
		for _, name := range []string{"README.md", "main.go", "notes.txt"} {
			if !strings.Contains(b, filepath.Join(ws, name)) {
				t.Errorf("%s lost %s:\n%s", f, name, b)
			}
		}
	}
	if b := readFileOrEmpty(filepath.Join(cfg, "workspaces.json")); !strings.Contains(b, `"`+ws+`"`) {
		t.Errorf("workspaces.json lost the workspace:\n%s", b)
	}
	// The old chat is still saved, with its messages.
	chats, _ := filepath.Glob(filepath.Join(cfg, "ai", "chats", "*.json"))
	var all string
	for _, c := range chats {
		all += readFileOrEmpty(c)
	}
	if !strings.Contains(all, oldChatAsk) || !strings.Contains(all, oldChatAnswer) {
		t.Errorf("the old chat is lost; chats: %v", chats)
	}
}
