package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// covHTTP is the covSvcHTTP workspace whose nvim can never reach the
// system clipboard: no display, and a clipboard provider that drops
// everything. Copies go to the returned sink.
func covHTTP(t *testing.T) (*Model, string, *[]string) {
	t.Helper()
	for _, k := range []string{"DISPLAY", "WAYLAND_DISPLAY"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	var clip []string
	t.Cleanup(SetClipboardSink(func(s string) { clip = append(clip, s) }))
	m, root, _ := covSvcHTTP(t)
	if err := m.nvim.ExecLua(`vim.g.clipboard = { name = 'none',
  copy = { ['+'] = function() end, ['*'] = function() end },
  paste = { ['+'] = function() return {} end, ['*'] = function() return {} end } }`); err != nil {
		t.Fatal(err)
	}
	return m, root, &clip
}

// covHTTPEdit opens file in the editor with the cursor on line.
func covHTTPEdit(t *testing.T, m *Model, file string, line int) {
	t.Helper()
	m.httpOpenInEditor(file)
	m.editor.SetMeta(file, false, "http")
	if line > 0 {
		if err := m.nvim.ExecLuaArgs(`local l = ...
pcall(vim.api.nvim_win_set_cursor, 0, { l, 0 })`, line); err != nil {
			t.Fatal(err)
		}
	}
}

// covHTTPReset closes every overlay so the next command starts clean.
func covHTTPReset(m *Model) {
	m.pickerOpen, m.promptOpen, m.confirmOpen = false, false, false
	m.toast = toast.New()
}

// covHTTPToast is the newest toast's title and detail ("" = none).
func covHTTPToast(m *Model) string {
	l := m.toast.Live()
	if len(l) == 0 {
		return ""
	}
	return l[len(l)-1].Title + " " + l[len(l)-1].Detail
}

func covHTTPDispatch(t *testing.T, m *Model, id string) tea.Cmd {
	t.Helper()
	cmd, ok := m.dispatchHTTPPalette(id)
	if !ok {
		t.Fatalf("%s not handled", id)
	}
	return cmd
}

// Without an open .http file and without any answer, every HTTP
// palette command says what is missing or opens its prompt.
func TestCovHTTPPaletteWithoutFile(t *testing.T) {
	m, root, _ := covHTTP(t)
	if _, ok := m.dispatchHTTPPalette("git-status"); ok {
		t.Fatal("a non-HTTP id was taken")
	}
	if _, ok := m.dispatchHTTPPalette("http-nope"); ok {
		t.Fatal("an unknown http- id was taken")
	}
	toasts := map[string]string{
		"http-send":          "No request yet",
		"http-cancel":        "No request is running",
		"http-last-response": "No response yet",
		"http-history":       "No HTTP history yet",
		"http-save":          "No request to save",
		"http-copy-curl":     "No request to copy",
		"http-export-har":    "Open a .http file first",
		"http-save-body":     "No response body to save",
		"http-fields":        "Open an .http file first",
		"http-ws-send":       "No WebSocket session",
		"http-ws-close":      "No WebSocket session",
		"http-proxy-set":     "Open an .http file first",
		"http-gql-schema":    "Open an .http file first",
	}
	for id, want := range toasts {
		covHTTPReset(m)
		covHTTPDispatch(t, m, id)
		if got := covHTTPToast(m); !strings.Contains(got, want) {
			t.Errorf("%s: toast %q, want %q", id, got, want)
		}
	}
	prompts := map[string]promptKindEnum{
		"http-quick":             promptKindHTTPQuick,
		"http-import-curl":       promptKindHTTPImport,
		"http-import-openapi":    promptKindHTTPImportOpenAPI,
		"http-import-collection": promptKindHTTPImportCollection,
		"http-new-file":          promptKindHTTPNewFile,
	}
	for id, kind := range prompts {
		covHTTPReset(m)
		covHTTPDispatch(t, m, id)
		if !m.promptOpen || m.promptKind != kind {
			t.Errorf("%s: prompt open %v kind %v", id, m.promptOpen, m.promptKind)
		}
		if id == "http-new-file" && m.prompt.Value() != "requests.http" {
			t.Errorf("new file default %q", m.prompt.Value())
		}
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-env-select")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPEnv || !strings.Contains(covSvcPickerText(m), "No environments") {
		t.Errorf("env picker: %s", covSvcPickerText(m))
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-saved")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPSaved || !strings.Contains(covSvcPickerText(m), "Open saved.http") {
		t.Errorf("saved picker: %s", covSvcPickerText(m))
	}
	// The env files are made from their templates; the private one is
	// 0600 and, with no git, asks nothing.
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-env-open")
	if b, err := os.ReadFile(filepath.Join(root, httpclient.EnvFile)); err != nil || !strings.Contains(string(b), "staging") {
		t.Errorf("env file: %v %q", err, b)
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-env-open-private")
	st, err := os.Stat(filepath.Join(root, httpclient.PrivateEnvFile))
	if err != nil || st.Mode().Perm() != 0o600 || m.confirmOpen {
		t.Errorf("private env file: %v %v card %v", st, err, m.confirmOpen)
	}
	// With env files the picker lists their environments.
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-env-select")
	if got := covSvcPickerText(m); !strings.Contains(got, "dev") || !strings.Contains(got, "staging") {
		t.Errorf("env picker: %s", got)
	}
	c, _ := m.handleHTTPPickerSelect("staging")
	covSvcHTTPRun(t, m, c)
	if httpSelectedEnv(root) != "staging" || !strings.Contains(covHTTPToast(m), "HTTP environment: staging") {
		t.Errorf("env = %q toast %q", httpSelectedEnv(root), covHTTPToast(m))
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-env-select")
	if it, ok := m.picker.Current(); !ok || it.ID != "staging" {
		t.Errorf("cursor not on the active env: %+v", it)
	}
	m.httpS().envRoot = ""
	m.handleHTTPPickerSelect("-")
	if httpSelectedEnv(root) != "" || !strings.Contains(covHTTPToast(m), "(none)") {
		t.Errorf("env cleared = %q toast %q", httpSelectedEnv(root), covHTTPToast(m))
	}
}

// The send picker walks the workspace for .http files and skips hidden
// and vendored folders; an empty workspace says so.
func TestCovHTTPSendPickerWalksWorkspace(t *testing.T) {
	m, root, _ := covHTTP(t)
	for _, d := range []string{".hidden", "node_modules", "sub"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, d, "x.http"), []byte("GET http://127.0.0.1:1/"+d+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := httpWorkspaceFiles(root, 40)
	if len(files) != 2 || !strings.HasSuffix(files[0], "api.http") || !strings.HasSuffix(files[1], filepath.Join("sub", "x.http")) {
		t.Fatalf("files = %v", files)
	}
	if got := httpWorkspaceFiles(root, 1); len(got) != 1 {
		t.Fatalf("max 1: %v", got)
	}
	covHTTPDispatch(t, m, "http-send-pick")
	got := covSvcPickerText(m)
	if !strings.Contains(got, "/sub") || strings.Contains(got, "node_modules") || strings.Contains(got, ".hidden") {
		t.Fatalf("picker:\n%s", got)
	}
	// Bad ids and a file that went away do nothing.
	for _, id := range []string{"x", "-1", "99"} {
		if c, ok := m.handleHTTPPickerSelect(id); !ok || c != nil {
			t.Errorf("%s: %v %v", id, c, ok)
		}
	}
	_ = os.Remove(filepath.Join(root, "sub", "x.http"))
	if c, _ := m.handleHTTPPickerSelect("2"); c != nil {
		t.Error("a gone file was sent")
	}
	// A workspace with no requests says so.
	for _, p := range []string{"api.http", filepath.Join(".hidden", "x.http"), filepath.Join("node_modules", "x.http")} {
		_ = os.Remove(filepath.Join(root, p))
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-send-pick")
	if m.pickerOpen || !strings.Contains(covHTTPToast(m), "No .http requests") {
		t.Errorf("empty workspace: open %v toast %q", m.pickerOpen, covHTTPToast(m))
	}
}

// In an open .http file the send key sends the request at the cursor:
// the unknown {{token}} asks first; the answer shows; the send key
// elsewhere sends the shown request again.
func TestCovHTTPSendAtCursorAndResend(t *testing.T) {
	m, root, _ := covHTTP(t)
	file := filepath.Join(root, "api.http")
	covHTTPEdit(t, m, file, 5)
	if !m.sendKeyBuffer() {
		t.Error("the .http buffer takes the send key")
	}
	if c := covHTTPDispatch(t, m, "http-send"); c != nil || !m.confirmOpen || m.confirmKind != confirmKindHTTPUnresolved {
		t.Fatalf("no send anyway card: %s", cardText(m))
	}
	if !strings.Contains(cardText(m), "{{token}}") {
		t.Errorf("card: %s", cardText(m))
	}
	c, _ := m.handleHTTPConfirm("send")
	covSvcHTTPRun(t, m, c)
	s := m.httpS()
	if s.last == nil || s.last.resp.Status != 200 {
		t.Fatalf("last = %+v", s.last)
	}
	// A second send while one runs is refused.
	s.running = s.last
	if covHTTPReset(m); m.httpSendRequest(file, nil, s.last.rr.Template, "") == nil || !strings.Contains(covHTTPToast(m), "still running") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	s.running = nil
	// Away from the .http file, the send key sends the shown run again.
	m.focus = FocusEditor
	m.editor.SetMeta(filepath.Join(root, "notes.txt"), false, "text")
	if m.sendKeyBuffer() {
		t.Error("a text buffer takes the send key")
	}
	m.focus = FocusExplorer
	if m.sendKeyBuffer() {
		t.Error("the explorer takes the send key")
	}
	covHTTPReset(m)
	if c := covHTTPDispatch(t, m, "http-send"); c != nil || m.confirmKind != confirmKindHTTPUnresolved {
		t.Fatal("resend did not ask about {{token}}")
	}
	m.handleHTTPConfirm("cancel")
	// A run whose request is no longer in its file sends its template.
	s.shown = &httpRun{file: file, label: "gone", rr: httpclient.Resolved{Template: httpclient.Request{Method: "GET", URL: s.last.rr.URL}}}
	c = m.httpResend()
	covSvcHTTPRun(t, m, c)
	if s.last.label != "GET /users" {
		t.Errorf("resent %q", s.last.label)
	}
	// A quick request (no file) is sent from its template.
	s.shown = &httpRun{rr: httpclient.Resolved{Template: httpclient.Request{Method: "GET", URL: s.last.rr.URL}}}
	covSvcHTTPRun(t, m, m.httpResend())
	s.shown, s.last = nil, nil
	if covHTTPReset(m); m.httpResend() == nil || !strings.Contains(covHTTPToast(m), "No request") {
		t.Errorf("resend without run: %q", covHTTPToast(m))
	}
	// Show Last Response opens the newest answer.
	covSvcHTTPRun(t, m, m.httpSendRequest("", nil, httpclient.Request{Method: "GET", URL: strings.TrimSuffix(s.ring[0].rr.URL, "/users") + "/users"}, ""))
	s.shown = nil
	covHTTPDispatch(t, m, "http-last-response")
	if s.shown == nil || s.shown.resp.Status != 200 {
		t.Errorf("shown = %+v", s.shown)
	}
}

// Quick requests, curl imports and file names from the prompts.
func TestCovHTTPPromptsQuickAndImport(t *testing.T) {
	m, root, _ := covHTTP(t)
	if c, ok := m.handleHTTPPromptSubmit("x"); ok || c != nil {
		t.Fatal("no prompt kind was taken")
	}
	m.promptKind = promptKindEnum(987654)
	if _, ok := m.handleHTTPPromptSubmit("x"); ok {
		t.Fatal("an unknown prompt kind was taken")
	}
	m.promptKind = promptKindHTTPQuick
	m.handleHTTPPromptSubmit("")
	if !strings.Contains(covHTTPToast(m), "Quick request:") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	srvURL := strings.TrimSuffix(strings.Fields(mustRead(t, filepath.Join(root, "api.http")))[2], "")
	r := covSvcHTTPQuick(t, m, "GET "+srvURL+"/users")
	if r.resp.Status != 200 {
		t.Fatalf("quick status %d", r.resp.Status)
	}
	// A bad curl says why.
	covHTTPReset(m)
	m.promptKind = promptKindHTTPImport
	m.handleHTTPPromptSubmit("wget x")
	if !strings.Contains(covHTTPToast(m), "Import from curl") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// Into a file that is not open: appended on disk after a ###.
	other := filepath.Join(root, "other.http")
	if err := os.WriteFile(other, []byte("GET http://127.0.0.1:1/a"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.httpS().importInto = other
	covSvcHTTPRun(t, m, func() tea.Cmd { c, _ := m.handleHTTPPromptSubmit("curl -X POST http://127.0.0.1:1/b -d x=1"); return c }())
	if got := mustRead(t, other); !strings.Contains(got, "GET http://127.0.0.1:1/a\n\n###\n") || !strings.Contains(got, "POST http://127.0.0.1:1/b") {
		t.Errorf("other.http:\n%s", got)
	}
	// Into the open buffer: the lines go in the buffer, the cursor on
	// the new request.
	file := filepath.Join(root, "api.http")
	covHTTPEdit(t, m, file, 1)
	m.httpS().importInto = file
	m.handleHTTPPromptSubmit("curl http://127.0.0.1:1/c")
	var line, text string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_get_current_line()`, &line)
	_ = m.nvim.ExecLuaResult(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &text)
	if line != "GET http://127.0.0.1:1/c" || !strings.Contains(text, "###\nGET http://127.0.0.1:1/c") {
		t.Errorf("cursor line %q, buffer:\n%s", line, text)
	}
	// No target file: scratch.http in the workspace.
	m.httpS().importInto = ""
	m.handleHTTPPromptSubmit("curl http://127.0.0.1:1/d")
	if got := mustRead(t, filepath.Join(root, "scratch.http")); !strings.HasPrefix(got, "GET http://127.0.0.1:1/d") {
		t.Errorf("scratch.http:\n%s", got)
	}
	// A file that cannot be written says so.
	m.httpS().importInto = filepath.Join(root, "nodir", "x", "y.http")
	m.handleHTTPPromptSubmit("curl http://127.0.0.1:1/e")
	if !strings.Contains(covHTTPToast(m), "Import failed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// New .http files: the name gets .http, the template is written once,
// an existing file is only opened.
func TestCovHTTPNewFile(t *testing.T) {
	m, root, _ := covHTTP(t)
	m.promptKind = promptKindHTTPNewFile
	if c, ok := m.handleHTTPPromptSubmit("  "); !ok || c != nil {
		t.Fatal("empty name did something")
	}
	m.handleHTTPPromptSubmit("calls/users")
	p := filepath.Join(root, "calls", "users.http")
	if got := mustRead(t, p); got != httpNewFileTemplate {
		t.Fatalf("new file:\n%s", got)
	}
	var name string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name)
	if name != p {
		t.Errorf("opened %q", name)
	}
	if err := os.WriteFile(p, []byte("GET http://x/kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.handleHTTPPromptSubmit(p)
	if got := mustRead(t, p); got != "GET http://x/kept\n" {
		t.Errorf("an existing file was rewritten:\n%s", got)
	}
	// A folder that is a file cannot hold the new file.
	m.handleHTTPPromptSubmit("api.http/x.http")
	if !strings.Contains(covHTTPToast(m), "Could not create") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.nvim = nil
	if m.httpOpenInEditor(p) != nil {
		t.Error("open without an editor")
	}
}

// Save Request keeps the template (secrets as placeholders) in
// saved.http; the Saved picker sends it, Ctrl+E and "__edit" open the
// file.
func TestCovHTTPSaveAndSaved(t *testing.T) {
	m, root, _ := covHTTP(t)
	file := filepath.Join(root, "api.http")
	// From the buffer when nothing was sent yet.
	covHTTPEdit(t, m, file, 10)
	covHTTPDispatch(t, m, "http-save")
	if !m.promptOpen || m.promptKind != promptKindHTTPSaveName || m.prompt.Value() != "missing" {
		t.Fatalf("save prompt %v %v %q", m.promptOpen, m.promptKind, m.prompt.Value())
	}
	m.handleHTTPPromptSubmit("mine")
	if got := mustRead(t, httpSavedPath()); !strings.Contains(got, "### mine") {
		t.Fatalf("saved.http:\n%s", got)
	}
	// From a run without a name the name is derived.
	s := m.httpS()
	s.shown = &httpRun{rr: httpclient.Resolved{Template: httpclient.Request{Method: "GET", URL: "http://127.0.0.1:1/things/7"}}}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-save")
	if m.prompt.Value() == "" {
		t.Error("no derived name")
	}
	m.promptKind = promptKindHTTPSaveName
	m.handleHTTPPromptSubmit("things")
	s.copyRun = nil
	if m.httpSaveRequest("x") != nil {
		t.Error("save without a run")
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-saved")
	got := covSvcPickerText(m)
	if !strings.Contains(got, "mine") || !strings.Contains(got, "things") {
		t.Fatalf("saved picker:\n%s", got)
	}
	for _, id := range []string{"x", "-1", "9"} {
		if c, _ := m.handleHTTPPickerSelect(id); c != nil {
			t.Errorf("%s sent", id)
		}
	}
	c, _ := m.handleHTTPPickerSelect("1")
	covSvcHTTPRun(t, m, c)
	if s.last == nil || s.last.resp == nil || s.last.resp.Err == "" {
		t.Errorf("saved send: %+v", s.last)
	}
	m.pickerKind = pickerKindHTTPSaved
	m.handleHTTPPickerSelect("__edit")
	var name string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name)
	if name != httpSavedPath() {
		t.Errorf("opened %q", name)
	}
	covHTTPDispatch(t, m, "http-saved")
	if c, ok := m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlE}); !ok || m.pickerOpen {
		t.Errorf("ctrl+e: %v %v", c, ok)
	}
	covHTTPDispatch(t, m, "http-saved")
	if _, ok := m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlX}); ok {
		t.Error("ctrl+x was taken")
	}
	// The saved file cannot be written when its folder is a file.
	s.copyRun = &httpRun{rr: httpclient.Resolved{Template: httpclient.Request{Method: "GET", URL: "http://x/"}}}
	_ = os.Remove(httpSavedPath())
	if err := os.MkdirAll(httpSavedPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	m.httpSaveRequest("x")
	if !strings.Contains(covHTTPToast(m), "Save failed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	cfg := filepath.Dir(httpSavedPath())
	_ = os.RemoveAll(cfg)
	if err := os.WriteFile(cfg, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.httpSaveRequest("x")
	if !strings.Contains(covHTTPToast(m), "Save failed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	_ = os.Remove(cfg)
}

// Save Response Body: default name from the URL and type, no
// overwrite, folders made.
func TestCovHTTPSaveBody(t *testing.T) {
	m, root, _ := covHTTP(t)
	s := m.httpS()
	s.shown = &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{URL: "http://h/a/users"}},
		resp: &httpclient.Response{URL: "http://h/b/list", IsJSON: true, Body: []byte(`[1]`)}}
	covHTTPDispatch(t, m, "http-save-body")
	if !m.promptOpen || m.prompt.Value() != "list.json" {
		t.Fatalf("prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	if n := httpBodyFileName(&httpRun{rr: httpclient.Resolved{Request: httpclient.Request{URL: "http://h/"}}, resp: &httpclient.Response{ContentType: "image/png"}}); n != "response.png" {
		t.Errorf("name %q", n)
	}
	m.promptKind = promptKindHTTPSaveBody
	m.handleHTTPPromptSubmit("out/list.json")
	if got := mustRead(t, filepath.Join(root, "out", "list.json")); got != "[1]" || !strings.Contains(covHTTPToast(m), "Saved out/list.json") {
		t.Fatalf("saved %q toast %q", got, covHTTPToast(m))
	}
	m.handleHTTPPromptSubmit("out/list.json")
	if !strings.Contains(covHTTPToast(m), "already exists") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.handleHTTPPromptSubmit(filepath.Join(root, "api.http", "x"))
	if !strings.Contains(covHTTPToast(m), "Save failed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	if err := os.MkdirAll(filepath.Join(root, "ro"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "ro"), 0o700) })
	if os.Geteuid() != 0 {
		m.handleHTTPPromptSubmit("ro/x.json")
		if !strings.Contains(covHTTPToast(m), "Save failed") {
			t.Errorf("toast %q", covHTTPToast(m))
		}
	}
	if m.httpSaveBody("") != nil {
		t.Error("empty name saved")
	}
}

// OpenAPI import from a file: errors say why, the first import writes
// <title>.http and the env file, the next one -2 and only a note.
func TestCovHTTPImportOpenAPIFile(t *testing.T) {
	m, root, _ := covHTTP(t)
	m.promptKind = promptKindHTTPImportOpenAPI
	if c, _ := m.handleHTTPPromptSubmit(" "); c != nil {
		t.Fatal("empty name did something")
	}
	m.handleHTTPPromptSubmit("nope.yaml")
	if !strings.Contains(covHTTPToast(m), "could not read nope.yaml") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	if err := os.WriteFile(filepath.Join(root, "bad.yaml"), []byte("hello: world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.handleHTTPPromptSubmit("bad.yaml")
	if !strings.Contains(covHTTPToast(m), "Import OpenAPI:") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	spec := "openapi: 3.0.0\ninfo: {title: Pets}\nservers: [{url: 'http://pets.local'}]\npaths:\n  /pets:\n    get:\n      operationId: listPets\n      responses: {\"200\": {description: ok}}\n"
	if err := os.WriteFile(filepath.Join(root, "pets.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	m.handleHTTPPromptSubmit("pets.yaml")
	if got := mustRead(t, filepath.Join(root, "pets.http")); !strings.Contains(got, "/pets") {
		t.Fatalf("pets.http:\n%s", got)
	}
	if !strings.Contains(mustRead(t, filepath.Join(root, httpclient.EnvFile)), "baseUrl") || !strings.Contains(covHTTPToast(m), "Imported 1 requests into pets.http") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.handleHTTPPromptSubmit(filepath.Join(root, "pets.yaml"))
	if _, err := os.Stat(filepath.Join(root, "pets-2.http")); err != nil || !strings.Contains(covHTTPToast(m), "set baseUrl") {
		t.Errorf("second import: %v toast %q", err, covHTTPToast(m))
	}
	// A folder that cannot be written.
	im, _ := httpclient.ImportOpenAPI([]byte(spec))
	m.httpWriteOpenAPI(im, filepath.Join(root, "missing-dir"))
	if !strings.Contains(covHTTPToast(m), "Import OpenAPI") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// Too big a file.
	big := filepath.Join(root, "big.json")
	if err := os.WriteFile(big, make([]byte, 20), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFileLimit(big, 10); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("limit: %v", err)
	}
	if _, err := readFileLimit(root, 10); err == nil {
		t.Error("a folder was read")
	}
}

// Private env file in a git repo: an unignored file asks to add it to
// .gitignore; Add writes the line; the buffer check warns once per root.
func TestCovHTTPPrivateEnvGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	m, root, _ := covHTTP(t)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "api.http")
	covHTTPEdit(t, m, file, 1)
	covHTTPDispatch(t, m, "http-env-open-private")
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPGitignore {
		t.Fatalf("no gitignore card: %s", cardText(m))
	}
	if c, _ := m.handleHTTPConfirm("cancel"); c != nil {
		t.Error("cancel did something")
	}
	// The buffer check warns (once) while the file is not ignored.
	cmd := m.httpCheckPrivateEnv(file)
	msg := cmd()
	pm, ok := msg.(httpPrivateEnvCheckMsg)
	if !ok || pm.state != httpGitUnignored {
		t.Fatalf("check msg = %#v", msg)
	}
	m.handleHTTPMsg(msg)
	if !strings.Contains(covHTTPToast(m), "not git-ignored") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	if m.httpCheckPrivateEnv(file) != nil {
		t.Error("warned twice")
	}
	m.handleHTTPMsg(httpPrivateEnvCheckMsg{root: root, rel: "x", state: httpGitTracked})
	if !strings.Contains(covHTTPToast(m), "tracked by git") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.confirmKind = confirmKindHTTPGitignore
	m.httpS().envRoot = root
	m.handleHTTPConfirm("add")
	if got := mustRead(t, filepath.Join(root, ".gitignore")); got != "*.log\n"+httpclient.PrivateEnvFile+"\n" {
		t.Errorf(".gitignore = %q", got)
	}
	if st := httpGitState(root, httpclient.PrivateEnvFile); st != httpGitIgnored {
		t.Errorf("state = %v", st)
	}
	// Now ignored: the check finds nothing to say.
	m.httpS().gitWarned = map[string]bool{}
	if msg := m.httpCheckPrivateEnv(file)(); msg != nil {
		t.Errorf("ignored file warned: %#v", msg)
	}
	if out, err := exec.Command("git", "-C", root, "add", "-f", httpclient.PrivateEnvFile).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if st := httpGitState(root, httpclient.PrivateEnvFile); st != httpGitTracked {
		t.Errorf("state = %v", st)
	}
	if st := httpGitState(t.TempDir(), "x"); st != httpGitNone {
		t.Errorf("no repo: %v", st)
	}
	// .gitignore that cannot be written.
	m.httpS().envRoot = filepath.Join(root, "api.http")
	m.handleHTTPConfirm("add")
	if !strings.Contains(covHTTPToast(m), ".gitignore not changed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// An env file in a sub folder is the one opened.
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, httpclient.EnvFile), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "s.http"), []byte("GET http://x/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covHTTPEdit(t, m, filepath.Join(sub, "s.http"), 1)
	covHTTPDispatch(t, m, "http-env-open")
	var name string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name)
	if name != filepath.Join(sub, httpclient.EnvFile) {
		t.Errorf("opened %q", name)
	}
}

// History: entries are grouped (pinned first), Enter sends again,
// Ctrl+O shows, Ctrl+T pins, Ctrl+S saves, Ctrl+D deletes, Clear asks.
func TestCovHTTPHistoryPicker(t *testing.T) {
	m, root, _ := covHTTP(t)
	file := filepath.Join(root, "api.http")
	f, _ := m.httpReadFile(file)
	covSvcHTTPRun(t, m, m.httpSendRequest(file, f.Vars, f.Requests[1], ""))
	srv := strings.TrimSuffix(m.httpS().last.rr.URL, "/nope")
	covSvcHTTPRun(t, m, m.httpSendRequest("", nil, httpclient.Request{Method: "GET", URL: srv + "/users"}, ""))
	ids := covSvcHistoryIDs(t)
	if len(ids) != 2 {
		t.Fatalf("history = %v", ids)
	}
	covHTTPDispatch(t, m, "http-history")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPHistory {
		t.Fatal("no history picker")
	}
	key := func(k tea.KeyType) (tea.Cmd, bool) { return m.httpPickerKey(tea.KeyMsg{Type: k}) }
	m.picker.SetCursorID(ids[0])
	if c, ok := key(tea.KeyCtrlT); !ok {
		t.Fatal("ctrl+t not taken")
	} else {
		covSvcHTTPRun(t, m, c)
	}
	if got := covSvcPickerText(m); !strings.Contains(got, "PINNED") {
		t.Errorf("no pinned group:\n%s", got)
	}
	m.picker.SetCursorID(ids[0])
	if _, ok := key(tea.KeyCtrlS); !ok || !m.promptOpen || m.promptKind != promptKindHTTPSaveName {
		t.Error("ctrl+s: no save prompt")
	}
	covHTTPDispatch(t, m, "http-history")
	m.picker.SetCursorID(ids[1])
	key(tea.KeyCtrlO)
	if s := m.httpS().shown; s == nil || s.file != file {
		t.Errorf("ctrl+o shown = %+v", s)
	}
	covHTTPDispatch(t, m, "http-history")
	m.picker.SetCursorID(ids[1])
	if _, ok := key(tea.KeyCtrlX); ok {
		t.Error("ctrl+x taken")
	}
	// Enter on an entry sends it again (from its file: same label).
	c, _ := m.handleHTTPPickerSelect(ids[1])
	covSvcHTTPRun(t, m, c)
	if l := m.httpS().last; l.resp.Status != 404 || l.file != file {
		t.Errorf("resend: %+v", l)
	}
	// An entry of a quick request (no file).
	c, _ = m.handleHTTPPickerSelect(ids[0])
	covSvcHTTPRun(t, m, c)
	if c, _ := m.handleHTTPPickerSelect("no-such-id"); c != nil {
		t.Error("an unknown entry was sent")
	}
	covHTTPDispatch(t, m, "http-history")
	m.picker.SetCursorID(ids[0])
	key(tea.KeyCtrlD)
	if got := covSvcHistoryIDs(t); len(got) != 3 {
		t.Errorf("after delete %v", got)
	}
	// Clear asks first.
	m.handleHTTPPickerSelect("__clear")
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPClearHistory {
		t.Fatal("no clear card")
	}
	m.handleHTTPConfirm("cancel")
	if len(covSvcHistoryIDs(t)) == 0 {
		t.Error("cancel cleared")
	}
	m.handleHTTPConfirm("clear")
	if len(covSvcHistoryIDs(t)) != 0 || !strings.Contains(covHTTPToast(m), "history cleared") {
		t.Errorf("clear: %v %q", covSvcHistoryIDs(t), covHTTPToast(m))
	}
	// Entry files: absolute, relative to the workspace, none.
	if got := httpEntryFile(httpclient.Entry{File: "a/b.http"}); got != filepath.Join(root, "a", "b.http") {
		t.Errorf("rel file %q", got)
	}
	if got := httpEntryFile(httpclient.Entry{File: "/x/y.http"}); got != "/x/y.http" {
		t.Errorf("abs file %q", got)
	}
	// AI entries say so in the hint.
	if err := httpHistoryStore().Append(httpclient.Entry{ID: "ai1", By: "ai", Env: "dev", Method: "GET", URL: "http://x/"}); err != nil {
		t.Fatal(err)
	}
	covHTTPDispatch(t, m, "http-history")
	if got := covSvcPickerText(m); !strings.Contains(got, "dev by AI") {
		t.Errorf("picker:\n%s", got)
	}
	// Keys on the Clear row, a gone entry, and a history that fails.
	m.picker.SetCursorID("__clear")
	if _, ok := key(tea.KeyCtrlO); ok {
		t.Error("ctrl+o on Clear taken")
	}
	m.picker.SetCursorID("ai1")
	_ = httpHistoryStore().Clear()
	for _, k := range []tea.KeyType{tea.KeyCtrlO, tea.KeyCtrlT, tea.KeyCtrlS} {
		if c, ok := key(k); !ok || c != nil {
			t.Errorf("key %v on a gone entry: %v %v", k, c, ok)
		}
	}
	_ = os.Remove(httpHistoryPath())
	if err := os.MkdirAll(httpHistoryPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-history")
	if !strings.Contains(covHTTPToast(m), "History failed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.pickerKind = pickerKindHTTPHistory
	m.pickerOpen = true
	m.handleHTTPConfirm("x")
	m.confirmKind = confirmKindHTTPClearHistory
	m.handleHTTPConfirm("clear")
	if !strings.Contains(covHTTPToast(m), "Clear failed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}

// The confirm router: unknown kinds are not ours; the copy card with
// no run or an unknown button does nothing.
func TestCovHTTPConfirmRouter(t *testing.T) {
	m, _, clip := covHTTP(t)
	m.confirmKind = confirmKindEnum(999999)
	if _, ok := m.handleHTTPConfirm("x"); ok {
		t.Error("unknown kind taken")
	}
	m.confirmKind = confirmKindHTTPCopySecrets
	if c, ok := m.handleHTTPConfirm("secrets"); !ok || c != nil {
		t.Error("copy without a run")
	}
	m.httpS().copyRun = &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "http://x/"}}}
	if c, _ := m.handleHTTPConfirm("cancel"); c != nil || len(*clip) != 0 {
		t.Error("cancel copied")
	}
	m.httpS().copyLang = "nope"
	if c, _ := m.handleHTTPConfirm("template"); c != nil || len(*clip) != 0 {
		t.Error("an unknown language copied")
	}
	m.httpS().copyLang = ""
	m.handleHTTPConfirm("template")
	if len(*clip) != 1 || !strings.HasPrefix((*clip)[0], "curl") {
		t.Errorf("clip = %q", *clip)
	}
	if got := httpSnippetTitle("nope"); got != "curl" {
		t.Errorf("title %q", got)
	}
	if m.httpCopySnippet(&httpRun{}, "nope") != nil {
		t.Error("an unknown language did something")
	}
	m.confirmKind = confirmKindHTTPUnresolved
	m.httpS().chainNext = []httpChainStep{{}}
	if c, _ := m.handleHTTPConfirm("send"); c != nil || m.httpS().chainNext != nil {
		t.Error("send without a pending run")
	}
}

// Copy as curl and Copy as Code at the cursor of an .http file.
func TestCovHTTPCopyAtCursor(t *testing.T) {
	m, root, clip := covHTTP(t)
	file := filepath.Join(root, "api.http")
	if err := os.WriteFile(file, []byte(mustRead(t, file)+"\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covHTTPEdit(t, m, file, 10)
	covHTTPDispatch(t, m, "http-copy-curl")
	if len(*clip) != 1 || !strings.Contains((*clip)[0], "/nope") {
		t.Fatalf("clip = %q", *clip)
	}
	covHTTPDispatch(t, m, "http-copy-snippet")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPSnippet {
		t.Fatal("no snippet picker")
	}
	// Above the first request there is none.
	if err := os.WriteFile(file, []byte("# only a comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.nvim.ExecLua(`vim.cmd('edit!')`)
	covHTTPEdit(t, m, file, 1)
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-copy-curl")
	if !strings.Contains(covHTTPToast(m), "no request") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-copy-snippet")
	if m.pickerOpen || !strings.Contains(covHTTPToast(m), "no request") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}
