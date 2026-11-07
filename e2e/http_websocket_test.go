//go:build e2e

package e2e

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// API-17: a WebSocket session through the real binary and terminal:
// send a WEBSOCKET request picked from an .http file, send a saved
// message (m) and a typed one (i), read the echoes on the response
// page, disconnect with Esc and close with q. The server is an
// httptest echo server (no network).
func TestHTTPWebSocket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, p, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), typ, []byte("echo "+string(p))); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	ws := NewRepo(t)
	if err := os.MkdirAll(filepath.Join(ws, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	wsURL := "ws://" + strings.TrimPrefix(srv.URL, "http://") + "/ws"
	src := "### chat\n# @name chat\nWEBSOCKET " + wsURL + "\n\nhello\n===\nping\n"
	if err := os.WriteFile(filepath.Join(ws, "api", "chat.http"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Cols: 100, Rows: 30, Workspace: ws})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.Palette("HTTP: Send Request…")
	tm.WaitText("chat", uiTimeout)
	tm.Type("chat")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("● connected · 0 sent · 0 received", uiTimeout)

	// m: a saved message from the request body.
	tm.Send("m")
	tm.WaitText("Type a message…", uiTimeout)
	tm.Type("ping")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("← echo ping", uiTimeout)
	tm.WaitText("● connected · 1 sent · 1 received", uiTimeout)

	// i: a typed message.
	tm.Send("i")
	tm.WaitText("Message:", uiTimeout)
	tm.Type("typed one")
	tm.Send(KeyEnter)
	tm.WaitText("← echo typed one", uiTimeout)
	tm.WaitText("● connected · 2 sent · 2 received", uiTimeout)

	// Esc disconnects; the log stays.
	tm.Send(KeyEsc)
	tm.WaitText("closed · 2 sent · 2 received", uiTimeout)
	tm.Send("q")
	tm.WaitGone("HTTP Response", uiTimeout)
	tm.Quit()
}
