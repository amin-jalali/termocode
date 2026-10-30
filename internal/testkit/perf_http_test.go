package testkit

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// PERF-8 perf guards of the HTTP client. The server is an httptest one
// that answers at once, so the numbers are termocode's own work.

// httpPerfDriver opens api/users.http (200 requests with variables, an
// auth header and dynamic values) in a workspace with env files and
// 1 000 history entries (the cap set to 1 000).
func httpPerfDriver(t *testing.T, base string) *Driver {
	t.Helper()
	var src strings.Builder
	src.WriteString("@host = " + base + "\n@token = {{secret}}\n\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&src, "### r%d\n# @name r%d\nPOST {{host}}/users/%d?ts={{$timestamp}}\nAuthorization: Bearer {{token}}\nContent-Type: application/json\n\n{\"id\": \"{{$guid}}\", \"n\": %d}\n\n", i, i, i, i)
	}
	var hist strings.Builder
	body := strings.Repeat("x", 900)
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&hist, `{"id":"%012d","time":"2026-10-30T00:00:00Z","by":"user","method":"GET","url":"{{host}}/u/%d","elapsed_ms":3,"size":900,"status":200,"body":"%s"}`+"\n", i, i, body)
	}
	files := textFiles()
	files["api/users.http"] = src.String()
	files["http-client.env.json"] = `{"$shared": {"secret": "dev-token-123456"}}`
	d := New(t, Options{W: 140, H: 40, Nvim: true, Files: files, Config: map[string]string{
		"config.json":        `{"http_history_max": 1000}`,
		"http/history.jsonl": hist.String(),
	}})
	open(d, "api/users.http")
	return d
}

// waitQuiet handles messages and checks cond (an nvim call) only when
// no message came for 2 ms, so the check does not slow down a stream of
// page batches it measures.
func waitQuiet(d *Driver, cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for d.pump(2 * time.Millisecond) {
		}
		if cond() {
			return true
		}
	}
	return false
}

func httpEchoServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":%q}`, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The send key on a request to its answer on the page: the median of 7
// sends of different requests.
func TestPerfHTTPSendToPage(t *testing.T) {
	perfbudget.Require(t)
	srv := httpEchoServer(t)
	d := httpPerfDriver(t, srv.URL)
	var took []time.Duration
	for i := 0; i < 8; i++ {
		n := 20 + 20*i
		open(d, "api/users.http")
		httpCursor(d, 6+8*n) // the request line of r<n>
		want := `"path": "/users/` + strconv.Itoa(n) + `"`
		t0 := time.Now()
		d.deliver(app.DevActionMsg{ID: "palette:http-send"})
		if !waitQuiet(d, func() bool { return strings.Contains(httpPageText(d), want) }, 10*time.Second) {
			t.Fatalf("send %d: page never showed %s:\n%s", i, want, httpPageText(d))
		}
		if i > 0 { // the first one opens the page
			took = append(took, time.Since(t0))
		}
		d.Settle(50 * time.Millisecond)
	}
	perfbudget.Check(t, "http_send_to_page", ms(percentile(took, 50)))
}

// A 5 MB JSON answer (840 000 pretty lines): the send to the page's
// first lines (what the user sees) and to every line in the buffer (the
// rest arrives in batches). Median of 5.
func TestPerfHTTPBigAnswerToPage(t *testing.T) {
	perfbudget.Require(t)
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := 0; sb.Len() < 5<<20; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id":%d,"name":"user %d","tags":["a","b"]}`, i, i)
	}
	sb.WriteString("]}")
	big := sb.String()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, big)
	}))
	t.Cleanup(srv.Close)
	d := httpPerfDriver(t, srv.URL)
	want := strconv.Itoa(strings.Count(string(mustIndent(big)), "\n") + 1 + 3)
	count := `local b = vim.g.termocode_http_buf; return b and vim.api.nvim_buf_is_valid(b) and tostring(vim.api.nvim_buf_line_count(b)) or '0'`
	var first, all []time.Duration
	for i := 0; i < 5; i++ {
		lua(d, `local b = vim.g.termocode_http_buf
if b and vim.api.nvim_buf_is_valid(b) then pcall(vim.cmd, 'bwipeout! ' .. b) end
vim.g.termocode_http_buf = nil
return ''`)
		open(d, "api/users.http")
		httpCursor(d, 6)
		t0 := time.Now()
		d.deliver(app.DevActionMsg{ID: "palette:http-send"})
		// Check after each message until the first lines show.
		if !d.WaitFor(func() bool { n, _ := strconv.Atoi(lua(d, count)); return n >= 1000 }, 30*time.Second) {
			t.Fatalf("no page lines: %s", lua(d, count))
		}
		first = append(first, time.Since(t0))
		if !waitQuiet(d, func() bool { return lua(d, count) == want }, 30*time.Second) {
			t.Fatalf("page lines %s, want %s", lua(d, count), want)
		}
		all = append(all, time.Since(t0))
		d.Settle(100 * time.Millisecond)
	}
	perfbudget.Check(t, "http_5mb_answer_first_lines", ms(percentile(first, 50)))
	perfbudget.Check(t, "http_5mb_answer_all_lines", ms(percentile(all, 50)))
}

// HTTP: History… with 1 000 entries to the picker on screen: the median
// of 5 opens.
func TestPerfHTTPHistoryOpen(t *testing.T) {
	perfbudget.Require(t)
	srv := httpEchoServer(t)
	d := httpPerfDriver(t, srv.URL)
	var took []time.Duration
	for i := 0; i < 5; i++ {
		dt, ok := waitFrame(d, app.DevActionMsg{ID: "palette:http-history"}, func(s string) bool { return strings.Contains(s, "/u/999") }, 10*time.Second)
		if !ok {
			t.Fatalf("history picker did not show:\n%s", screen(d))
		}
		took = append(took, dt)
		d.Key("esc")
		d.Settle(50 * time.Millisecond)
	}
	perfbudget.Check(t, "http_history_open_1000", ms(percentile(took, 50)))
}
