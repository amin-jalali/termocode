package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// The HTTP prompt router hands every database and Kubernetes prompt it
// fronts to that prompt's handler (empty text, no connection: each
// handler says so or does nothing, none panics). The Kubernetes logs
// prompt and the schema files picker are left out: they only open with
// their view's state in place.
func TestCovHTTPPromptRouterOtherPrompts(t *testing.T) {
	m, _, _ := covHTTP(t)
	kinds := []promptKindEnum{
		promptKindDBImportConnPath, promptKindDBDumpToolPath, promptKindDBRoleName, promptKindDBExportPath,
		promptKindDBRedisAdd, promptKindDBTSQLt, promptKindDBPgTAP, promptKindDBSSHHost, promptKindDBSchemaFolder,
		promptKindDBRoutineArg, promptKindDBRename, promptKindDBParam, promptKindDBSavepoint, promptKindDBOpenCSV,
		promptKindDBStopSession, promptKindKubeSaveScreen, promptKindDBAIGenerate,
		promptKindDBEditCell, promptKindDBLOBLoad, promptKindDBGotoRow, promptKindDBDataCompareKey,
		promptKindDBWhereValue, promptKindDBResultFilter, promptKindDBAdd, promptKindDBPassword,
	}
	for _, k := range kinds {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("kind %d panicked: %v", k, r)
				}
			}()
			covHTTPReset(m)
			m.promptKind = k
			if _, ok := m.handleHTTPPromptSubmit(""); !ok {
				t.Errorf("kind %d not routed", k)
			}
		})
	}
}

// The HTTP picker router does the same for the database pickers.
func TestCovHTTPPickerRouterOtherPickers(t *testing.T) {
	m, _, _ := covHTTP(t)
	for _, c := range []struct {
		kind pickerKindEnum
		id   string
	}{
		{pickerKindDBSave, "db-save-project"}, {pickerKindDBType, "nope"},
		{pickerKindDBTestResult, "db-test-password"}, {pickerKindDBTestResult, "db-test-retry"},
		{pickerKindDBTestResult, "db-test-ssh"}, {pickerKindDBTestResult, "db-test-ssl"},
		{pickerKindDBTestResult, "db-test-cancel"}, {pickerKindDBTestResult, "other"},
		{pickerKindDBSSHAuth, "x"},
	} {
		t.Run(fmt.Sprint(c.kind, c.id), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%d %s panicked: %v", c.kind, c.id, r)
				}
			}()
			covHTTPReset(m)
			m.pickerKind = c.kind
			if _, ok := m.handleHTTPPickerSelect(c.id); !ok {
				t.Errorf("%d %s not routed", c.kind, c.id)
			}
		})
	}
	m.pickerKind = pickerKindEnum(987654)
	if _, ok := m.handleHTTPPickerSelect("x"); ok {
		t.Error("an unknown picker was taken")
	}
}

// "Send without" on the dependency card sends the request with its
// {{login.response…}} placeholders left as they are; other unknown
// names still ask.
func TestCovHTTPChainDepSendWithout(t *testing.T) {
	m, file, hits := chainDepFixture(t)
	f, me := chainDepReq(t, m, file, "me")
	m.httpSendRequest(file, f.Vars, me, "")
	c, _ := m.handleHTTPConfirm("without")
	covSvcHTTPRun(t, m, c)
	if got := hits(); len(got) != 1 || !strings.HasPrefix(got[0], "/me Bearer {{login.response") {
		t.Fatalf("hits = %q", got)
	}
	// A dependency that is no longer in the file.
	m.httpSendRequest(file, f.Vars, me, "")
	if err := os.WriteFile(file, []byte("GET http://127.0.0.1:1/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covHTTPReset(m)
	m.handleHTTPConfirm("first")
	if !strings.Contains(covHTTPToast(m), "No request named login") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	if m.httpChainDepConfirm("first") != nil {
		t.Error("an answer with no card did something")
	}
}

// The send picker lists the open file only; the env picker reads the
// folder of the shown answer's file; failures to save say so.
func TestCovHTTPPickersMore(t *testing.T) {
	m, root, _ := covHTTP(t)
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(sub, "o.http")
	if err := os.WriteFile(other, []byte("###\n# @name only\nGET http://127.0.0.1:1/o\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, httpclient.EnvFile), []byte(`{"subenv":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	covHTTPEdit(t, m, other, 3)
	covHTTPDispatch(t, m, "http-send-pick")
	if got := covSvcPickerText(m); !strings.Contains(got, "only") || strings.Contains(got, "users") {
		t.Errorf("picker:\n%s", got)
	}
	// From the workspace (no .http file open): the picked request's
	// file lost its requests; a file that cannot be read is left out.
	_ = m.nvim.ExecLua(`vim.cmd('enew')`)
	_ = m.nvim.ExecLuaArgs(`local f = ...
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_get_name(b) == f then vim.api.nvim_buf_delete(b, { force = true }) end
end`, other)
	m.editor.SetMeta(filepath.Join(root, "notes.txt"), false, "text")
	covHTTPReset(m)
	m.openHTTPSendPicker()
	if err := os.WriteFile(filepath.Join(root, "api.http"), []byte("# none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, ok := m.handleHTTPPickerSelect("0"); !ok || c != nil {
		t.Error("a request that is gone was sent")
	}
	_ = os.Chmod(other, 0o000)
	t.Cleanup(func() { _ = os.Chmod(other, 0o644) })
	if os.Geteuid() != 0 {
		covHTTPReset(m)
		m.openHTTPSendPicker()
		if m.pickerOpen {
			t.Error("an unreadable file was listed")
		}
	}
	_ = os.Chmod(other, 0o644)
	// The env picker of a shown answer from sub/.
	m.editor.SetMeta(filepath.Join(root, "notes.txt"), false, "text")
	m.httpS().shown = &httpRun{file: other}
	covHTTPDispatch(t, m, "http-env-select")
	if got := covSvcPickerText(m); !strings.Contains(got, "subenv") {
		t.Errorf("env picker:\n%s", got)
	}
	// The state file cannot be written.
	st := httpStatePath()
	_ = os.Remove(st)
	if err := os.MkdirAll(st, 0o700); err != nil {
		t.Fatal(err)
	}
	covHTTPReset(m)
	m.handleHTTPPickerSelect("subenv")
	if !strings.Contains(covHTTPToast(m), "Could not save the environment") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// A new file whose folder cannot be written.
	ro := filepath.Join(root, "ro")
	if err := os.MkdirAll(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	if os.Geteuid() != 0 {
		covHTTPReset(m)
		m.httpNewFile(filepath.Join(ro, "x.http"))
		if !strings.Contains(covHTTPToast(m), "Could not create") {
			t.Errorf("toast %q", covHTTPToast(m))
		}
	}
}

// A delete the history store cannot write says so; an entry whose request
// is no longer in its file is sent from the entry itself.
func TestCovHTTPHistoryStoreFailures(t *testing.T) {
	m, root, _ := covHTTP(t)
	file := filepath.Join(root, "api.http")
	f, req := chainDepReq(t, m, file, "missing")
	covSvcHTTPRun(t, m, m.httpSendRequest(file, f.Vars, req, ""))
	ids := covSvcHistoryIDs(t)
	e, ok := httpHistoryStore().Get(ids[0])
	if !ok {
		t.Fatal("no entry")
	}
	if err := os.WriteFile(file, []byte("###\n# @name other\nGET http://127.0.0.1:1/z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covSvcHTTPRun(t, m, m.httpResendEntry(e))
	if l := m.httpS().last; l.label != "missing" || !strings.HasSuffix(l.rr.URL, "/nope") {
		t.Errorf("resent %q %q", l.label, l.rr.URL)
	}
	covHTTPDispatch(t, m, "http-history")
	m.picker.SetCursorID(ids[0])
	// The history file turns into a folder: delete fails.
	_ = os.Remove(httpHistoryPath())
	if err := os.MkdirAll(httpHistoryPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	covHTTPReset(m)
	m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	if !strings.Contains(covHTTPToast(m), "Delete failed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}

// Import OpenAPI from a URL: downloaded, written into the workspace;
// a failed download and a page that is no spec say why.
func TestCovHTTPImportOpenAPIURL(t *testing.T) {
	m, root, _ := covHTTP(t)
	spec := "openapi: 3.0.0\ninfo: {title: Remote}\npaths:\n  /r:\n    get:\n      operationId: getR\n      responses: {\"200\": {description: ok}}\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.yaml":
			_, _ = w.Write([]byte(spec))
		case "/page":
			_, _ = w.Write([]byte("<html>hi</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	m.promptKind = promptKindHTTPImportOpenAPI
	c, _ := m.handleHTTPPromptSubmit(srv.URL + "/openapi.yaml")
	covSvcHTTPRun(t, m, c)
	if got := mustRead(t, filepath.Join(root, "remote.http")); !strings.Contains(got, "/r") {
		t.Errorf("remote.http:\n%s", got)
	}
	c, _ = m.handleHTTPPromptSubmit(srv.URL + "/gone.yaml")
	covSvcHTTPRun(t, m, c)
	if !strings.Contains(covHTTPToast(m), "could not download the spec") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	c, _ = m.handleHTTPPromptSubmit(srv.URL + "/page")
	covSvcHTTPRun(t, m, c)
	if !strings.Contains(covHTTPToast(m), "Import OpenAPI:") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}
