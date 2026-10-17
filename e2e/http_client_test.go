//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// HC-12: the HTTP client through the real binary and terminal: send a
// request picked from an .http file, read the HTTP Response page, close
// it with q. The API is an httptest server (no network).
func TestHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":42,"name":"Ada Lovelace"}`)
	}))
	defer srv.Close()
	ws := NewRepo(t)
	if err := os.MkdirAll(filepath.Join(ws, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "@host = " + srv.URL + "\n\n### Get one user\n# @name getUser\nGET {{host}}/users/42\n"
	if err := os.WriteFile(filepath.Join(ws, "api", "users.http"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Cols: 100, Rows: 30, Workspace: ws})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	// No file open: the picker lists the requests of every .http file.
	tm.Palette("HTTP: Send Request…")
	tm.WaitText("getUser", uiTimeout)
	tm.Type("getUser")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("200 OK", uiTimeout)
	tm.WaitText(`"name": "Ada Lovelace"`, uiTimeout)
	tm.settle()
	tm.CheckScreen("http-response", mainFrame)
	tm.Send("q")
	tm.WaitGone("HTTP Response", uiTimeout)
	tm.Quit()
}
