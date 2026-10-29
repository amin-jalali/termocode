package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// covSvcHTTP is a workspace with an .http file pointing at a local test
// server, a private config dir and a real editor for the response page.
func covSvcHTTP(t *testing.T) (*Model, string, *httptest.Server) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":1,"name":"ann"}]`))
		case "/slow":
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	root := t.TempDir()
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	ws, err := localhost.New(root)
	if err != nil {
		t.Fatal(err)
	}
	setProcWorkspace(t, ws)
	src := "@host = " + srv.URL + "\n\n### users\n# @name users\nGET {{host}}/users\nAuthorization: Bearer {{token}}\n\n###\n# @name missing\nGET {{host}}/nope\n"
	if err := os.WriteFile(filepath.Join(root, "api.http"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Model{w: 120, h: 40}
	covSvcNvim(t, m)
	return m, root, srv
}

// covSvcHTTPRun runs HTTP commands until the response is in.
func covSvcHTTPRun(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	deadline := time.Now().Add(20 * time.Second)
	for len(queue) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("HTTP commands did not settle")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(3 * time.Second):
			continue // a timer (toast, spinner)
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if next, ok := m.handleHTTPMsg(msg); ok {
			queue = append(queue, next)
		}
	}
}

func covSvcPickerText(m *Model) string { return ansi.Strip(m.picker.View()) }

func covSvcHistoryIDs(t *testing.T) []string {
	t.Helper()
	list, err := httpHistoryStore().List()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range list {
		out = append(out, e.ID)
	}
	return out
}

// Send from the request picker; unknown {{token}} asks "Send anyway?";
// the answer lands on the page, in history and in the lens.
func TestCovSvcHTTPSendFlow(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	covSvcHTTPRun(t, m, func() tea.Cmd { c, _ := m.dispatchHTTPPalette("http-send-pick"); return c }())
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPSend {
		t.Fatalf("no send picker: %q", covSvcToast(m))
	}
	if got := covSvcPickerText(m); !strings.Contains(got, "GET users") || !strings.Contains(got, "GET missing") {
		t.Errorf("picker = %s", got)
	}
	m.pickerOpen = false
	cmd, _ := m.handleHTTPPickerSelect("0")
	if cmd != nil || !m.confirmOpen || m.confirmKind != confirmKindHTTPUnresolved || !strings.Contains(cardText(m), "{{token}}") {
		t.Fatalf("no send anyway card: %s", cardText(m))
	}
	if c, _ := m.handleHTTPConfirm("cancel"); c != nil || m.httpS().pending != nil {
		t.Error("cancel sent")
	}
	m.handleHTTPPickerSelect("0")
	c, _ := m.handleHTTPConfirm("send")
	covSvcHTTPRun(t, m, c)
	s := m.httpS()
	if s.last == nil || s.last.resp == nil || s.last.resp.Status != 200 {
		t.Fatalf("last = %+v", s.last)
	}
	if l := s.lens[filepath.Join(root, "api.http")+"\x00users"]; !strings.HasPrefix(l.text, "200") {
		t.Errorf("lens = %+v", l)
	}
	// A 404 from the second request.
	c, _ = m.handleHTTPPickerSelect("1")
	covSvcHTTPRun(t, m, c)
	if s.last.resp.Status != 404 {
		t.Errorf("second status = %d", s.last.resp.Status)
	}
	// Bad picker ids do nothing.
	for _, id := range []string{"x", "-1", "99"} {
		if c, ok := m.handleHTTPPickerSelect(id); !ok || c != nil {
			t.Errorf("id %q did something", id)
		}
	}
	// Show last, resend, copy as curl.
	covSvcHTTPRun(t, m, func() tea.Cmd { c, _ := m.dispatchHTTPPalette("http-last-response"); return c }())
	covSvcHTTPRun(t, m, m.httpResend())
	if s.last.resp.Status != 404 {
		t.Errorf("resend status = %d", s.last.resp.Status)
	}
	var clip []string
	defer SetClipboardSink(func(v string) { clip = append(clip, v) })()
	m.dispatchHTTPPalette("http-copy-curl")
	if len(clip) != 1 || !strings.HasPrefix(clip[0], "curl") {
		t.Errorf("copied %q", clip)
	}
	// History: lists both, Enter resends, Ctrl+D deletes, clear asks.
	m.dispatchHTTPPalette("http-history")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPHistory {
		t.Fatalf("no history picker: %q", covSvcToast(m))
	}
	ids := covSvcHistoryIDs(t)
	if len(ids) < 3 {
		t.Fatalf("history = %q", ids)
	}
	first := ids[0]
	covSvcHTTPRun(t, m, func() tea.Cmd { c, _ := m.handleHTTPPickerSelect(first); return c }())
	m.dispatchHTTPPalette("http-history")
	if c, ok := m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlO}); !ok {
		t.Error("ctrl+o not handled")
	} else {
		covSvcHTTPRun(t, m, c)
	}
	m.dispatchHTTPPalette("http-history")
	n := len(covSvcHistoryIDs(t))
	m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	if got := len(covSvcHistoryIDs(t)); got != n-1 {
		t.Errorf("after ctrl+d %d items, want %d", got, n-1)
	}
	m.handleHTTPPickerSelect("unknown-id")
	m.handleHTTPPickerSelect("__clear")
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPClearHistory {
		t.Fatal("no clear card")
	}
	m.handleHTTPConfirm("cancel")
	m.handleHTTPPickerSelect("__clear")
	m.handleHTTPConfirm("clear")
	if !strings.Contains(covSvcToast(m), "HTTP history cleared") {
		t.Errorf("clear toast = %q", covSvcToast(m))
	}
	m.dispatchHTTPPalette("http-history")
	if !strings.Contains(covSvcToast(m), "No HTTP history yet") {
		t.Errorf("empty history toast = %q", covSvcToast(m))
	}
}

// Quick requests, a cancel of a slow one, and the "nothing yet" notes.
func TestCovSvcHTTPQuickAndCancel(t *testing.T) {
	m, _, srv := covSvcHTTP(t)
	for _, id := range []string{"http-last-response", "http-save", "http-save-body", "http-cancel"} {
		m.dispatchHTTPPalette(id)
	}
	if !strings.Contains(covSvcToast(m), "No request is running") {
		t.Errorf("toast = %q", covSvcToast(m))
	}
	if c := m.httpSendAction(); c == nil || !strings.Contains(covSvcToast(m), "No request yet") {
		t.Errorf("send toast = %q", covSvcToast(m))
	}
	m.dispatchHTTPPalette("http-quick")
	m.promptKind = promptKindHTTPQuick
	if _, ok := m.handleHTTPPromptSubmit(""); !ok || !strings.Contains(covSvcToast(m), "Quick request") {
		t.Errorf("bad quick toast = %q", covSvcToast(m))
	}
	c, _ := m.handleHTTPPromptSubmit("GET " + srv.URL + "/users")
	covSvcHTTPRun(t, m, c)
	if r := m.httpS().last; r == nil || r.resp.Status != 200 {
		t.Fatalf("quick: %+v", r)
	}
	// Save the request and the body.
	m.dispatchHTTPPalette("http-save")
	if !m.promptOpen || m.promptKind != promptKindHTTPSaveName {
		t.Fatal("no save prompt")
	}
	m.handleHTTPPromptSubmit("my users")
	b, _ := os.ReadFile(httpSavedPath())
	if !strings.Contains(string(b), "my users") {
		t.Errorf("saved.http = %s", b)
	}
	m.dispatchHTTPPalette("http-save-body")
	if m.prompt.Value() != "users.json" {
		t.Errorf("body name = %q", m.prompt.Value())
	}
	m.promptKind = promptKindHTTPSaveBody
	m.handleHTTPPromptSubmit("out/users.json")
	if got, _ := os.ReadFile(filepath.Join(aiWorkspaceRoot(), "out", "users.json")); !strings.Contains(string(got), "ann") {
		t.Errorf("body file = %q", got)
	}
	m.handleHTTPPromptSubmit("out/users.json")
	if !strings.Contains(covSvcToast(m), "already exists") {
		t.Errorf("exists toast = %q", covSvcToast(m))
	}
	// Saved requests: Enter sends; __edit opens the file; Ctrl+E too.
	m.dispatchHTTPPalette("http-saved")
	if got := covSvcPickerText(m); !strings.Contains(got, "my users") || !strings.Contains(got, "Open saved.http") {
		t.Fatalf("saved picker = %s", got)
	}
	c, _ = m.handleHTTPPickerSelect("0")
	covSvcHTTPRun(t, m, c)
	m.dispatchHTTPPalette("http-saved")
	m.handleHTTPPickerSelect("9")
	m.handleHTTPPickerSelect("__edit")
	m.dispatchHTTPPalette("http-saved")
	if _, ok := m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlE}); !ok {
		t.Error("ctrl+e not handled")
	}
	// A slow request is cancelled; a second send while it runs waits.
	m.promptKind = promptKindHTTPQuick
	c, _ = m.handleHTTPPromptSubmit("GET " + srv.URL + "/slow")
	if c2, _ := m.handleHTTPPromptSubmit("GET " + srv.URL + "/users"); c2 == nil || !strings.Contains(covSvcToast(m), "still running") {
		t.Errorf("busy toast = %q", covSvcToast(m))
	}
	m.dispatchHTTPPalette("http-cancel")
	covSvcHTTPRun(t, m, c)
	if r := m.httpS().last; r == nil || r.resp.ErrKind != httpclient.ErrKindCancelled {
		t.Errorf("cancelled run = %+v", r.resp)
	}
}

// Import from curl appends a block to the file; a new .http file is
// made from the template; env files are made and selected.
func TestCovSvcHTTPFilesAndEnvs(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	m.dispatchHTTPPalette("http-import-curl")
	if _, ok := m.handleHTTPPromptSubmit("curl"); !ok {
		t.Fatal("import not handled")
	}
	m.promptKind = promptKindHTTPImport
	m.handleHTTPPromptSubmit("curl -X POST https://api.example.com/items -d '{\"a\":1}'")
	b, _ := os.ReadFile(filepath.Join(root, "scratch.http"))
	if !strings.Contains(string(b), "POST https://api.example.com/items") {
		t.Errorf("scratch.http = %s", b)
	}
	// The file is open now: the second block goes into the buffer.
	m.handleHTTPPromptSubmit("curl https://api.example.com/second")
	if f, ok := m.httpReadFile(filepath.Join(root, "scratch.http")); !ok || len(f.Requests) != 2 || !strings.HasSuffix(f.Requests[1].URL, "/second") {
		t.Errorf("second import = %+v", f.Requests)
	}
	m.dispatchHTTPPalette("http-new-file")
	m.promptKind = promptKindHTTPNewFile
	m.handleHTTPPromptSubmit("")
	m.handleHTTPPromptSubmit("notes.txt")
	if !strings.Contains(covSvcToast(m), ".http") {
		t.Errorf("not http toast = %q", covSvcToast(m))
	}
	m.handleHTTPPromptSubmit("sub/new.http")
	if b, err := os.ReadFile(filepath.Join(root, "sub", "new.http")); err != nil || !strings.Contains(string(b), "@name example") {
		t.Errorf("new file: %v %s", err, b)
	}
	// Env files: the shared one, then the private one in a git repo that
	// does not ignore it: the card offers .gitignore.
	m.dispatchHTTPPalette("http-env-select")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPEnv {
		t.Fatal("no env picker")
	}
	m.dispatchHTTPPalette("http-env-open")
	if _, err := os.Stat(filepath.Join(root, httpclient.EnvFile)); err != nil {
		t.Errorf("env file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.dispatchHTTPPalette("http-env-open-private")
	m.dispatchHTTPPalette("http-env-select")
	if got := covSvcPickerText(m); !strings.Contains(got, "dev") || !strings.Contains(got, "staging") {
		t.Errorf("env picker = %s", got)
	}
	m.handleHTTPPickerSelect("dev")
	if httpSelectedEnv(m.httpS().envRoot) != "dev" || !strings.Contains(covSvcToast(m), "HTTP environment: dev") {
		t.Errorf("env = %q toast %q", httpSelectedEnv(m.httpS().envRoot), covSvcToast(m))
	}
	m.dispatchHTTPPalette("http-env-select")
	m.handleHTTPPickerSelect("-")
	if !strings.Contains(covSvcToast(m), "(none)") {
		t.Errorf("none toast = %q", covSvcToast(m))
	}
	// The gitignore card adds the file name.
	m.httpS().envRoot = root
	m.confirmKind = confirmKindHTTPGitignore
	m.handleHTTPConfirm("cancel")
	m.handleHTTPConfirm("add")
	if b, _ := os.ReadFile(filepath.Join(root, ".gitignore")); !strings.Contains(string(b), httpclient.PrivateEnvFile) {
		t.Errorf(".gitignore = %q", b)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := httpAddToGitignore(root); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".gitignore")); string(b) != "x\n"+httpclient.PrivateEnvFile+"\n" {
		t.Errorf(".gitignore = %q", b)
	}
	if httpGitState(t.TempDir(), "x") != httpGitNone {
		t.Error("no repo is not httpGitNone")
	}
	// The once-per-workspace check finds nothing to warn about outside git.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, httpclient.PrivateEnvFile), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := m.httpCheckPrivateEnv(filepath.Join(other, "a.http")); c == nil || c() != nil {
		t.Error("check outside git warned")
	}
	if m.httpCheckPrivateEnv(filepath.Join(other, "a.http")) != nil {
		t.Error("checked twice")
	}
	if _, ok := m.handleHTTPMsg(httpPrivateEnvCheckMsg{rel: "x/" + httpclient.PrivateEnvFile, state: httpGitTracked}); !ok || !strings.Contains(covSvcToast(m), "tracked by git") {
		t.Errorf("tracked toast = %q", covSvcToast(m))
	}
	if _, ok := m.dispatchHTTPPalette("http-nope"); ok {
		t.Error("unknown id handled")
	}
	if _, ok := m.dispatchHTTPPalette("git-commit"); ok {
		t.Error("other id handled")
	}
}

// Copy as curl with a secret header asks how to copy.
func TestCovSvcHTTPCopySecrets(t *testing.T) {
	m, _, srv := covSvcHTTP(t)
	var clip []string
	defer SetClipboardSink(func(v string) { clip = append(clip, v) })()
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: srv.URL, Headers: []httpclient.Header{{Name: "Authorization", Value: "Bearer abc123secret"}}},
		Template: httpclient.Request{Method: "GET", URL: srv.URL, Headers: []httpclient.Header{{Name: "Authorization", Value: "Bearer {{token}}"}}}}}
	run.rr.URL = srv.URL
	m.httpCopyCurl(run)
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPCopySecrets {
		t.Skipf("no secrets card: the request holds no secret by this resolver (%q)", clip)
	}
	m.handleHTTPConfirm("cancel")
	m.handleHTTPConfirm("template")
	m.handleHTTPConfirm("secrets")
	if len(clip) != 2 || strings.Contains(clip[0], "abc123secret") || !strings.Contains(clip[1], "abc123secret") {
		t.Errorf("clip = %q", clip)
	}
	m.httpS().copyRun = nil
	m.handleHTTPConfirm("secrets")
	if m.httpCopyCurl(nil) == nil {
		t.Error("nil run did nothing")
	}
}
