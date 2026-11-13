//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestSendAttached sends chat.send with attaches and runs its
// background read and the message it gives on the Model, as Update
// would. It returns the answer frame.
func webTestSendAttached(t *testing.T, m *Model, c *webConn, args string) map[string]any {
	t.Helper()
	call, cmd := webTestCallLater(t, m, c, webproto.OpChatSend, args)
	if f := webTestAnswer(t, c, call); f != nil {
		return f // refused on Update
	}
	if cmd == nil {
		t.Fatal("no answer and no work")
	}
	if msg := cmd(); msg != nil {
		if _, ok := m.handleWebMsg(msg); !ok {
			t.Fatalf("message %T not handled", msg)
		}
	}
	f := webTestAnswer(t, c, call)
	if f == nil {
		t.Fatal("chat.send gave no answer")
	}
	return f
}

// webTestAttachModel is a chat model on a git repo with a fake AI.
func webTestAttachModel(t *testing.T) (*Model, *webConn, string) {
	t.Helper()
	m := webTestChatModel(t)
	webTestFakeAI(&m, "ok")
	webTestHub(t, &m)
	dir, _ := webTestRepo(t, &m)
	c := webTestSub(t, &m, "chat")
	return &m, c, dir
}

// Spec (MOB-5): a lines attach is read on the laptop: the model gets the
// lines as hidden context, the message shows one chip with its label,
// and the page's text is the message text.
func TestChatSendAttachLines(t *testing.T) {
	m, c, dir := webTestAttachModel(t)
	webTestWrite(t, dir, "src/main.go", "package main\n\nfunc main() {\n\tpanic(\"x\")\n}\n")
	f := webTestSendAttached(t, m, c, `{"text":"Why does this panic?","attach":[{"kind":"lines","path":"src/main.go","from":3,"to":5,"label":"main.go:3-5"}]}`)
	if f["t"] != "res" {
		t.Fatalf("send: %v", f)
	}
	if len(m.ai.conv.Messages) == 0 {
		t.Fatal("nothing sent")
	}
	um := m.ai.conv.Messages[0]
	if um.Text != "Why does this panic?" || !strings.Contains(um.Context, "func main() {\n\tpanic(\"x\")\n}") || strings.Contains(um.Context, "package main") {
		t.Fatalf("sent %+v", um)
	}
	if len(um.Attached) != 1 || um.Attached[0] != (ai.StoredAttach{Kind: "lines", Label: "main.go:3-5"}) {
		t.Fatalf("chips %+v", um.Attached)
	}
	cm, _ := webChatMsg(0, &um, false)
	if len(cm.Attached) != 1 || cm.Attached[0].Label != "main.go:3-5" {
		t.Fatalf("page message %+v", cm)
	}
	if len(m.ai.attachments) != 0 {
		t.Fatalf("attachments left %+v", m.ai.attachments)
	}
}

// Spec (MOB-5): a secret-like file never leaves the laptop: a .env file,
// lines or diff of it, is refused with "stays on the laptop" and nothing
// is sent; a path outside the workspace is refused too.
func TestChatSendAttachSecretStays(t *testing.T) {
	m, c, dir := webTestAttachModel(t)
	webTestWrite(t, dir, ".env", "TOKEN=abc\n")
	webTestWrite(t, dir, "config/id_rsa", "KEY\n")
	for _, a := range []string{
		`{"kind":"file","path":".env"}`,
		`{"kind":"lines","path":".env","from":1,"to":1}`,
		`{"kind":"diff","path":".env"}`,
		`{"kind":"file","path":"config/id_rsa"}`,
	} {
		f := webTestSendAttached(t, m, c, `{"text":"look","attach":[{"kind":"file","path":"a.txt"},`+a+`]}`)
		if webTestErr(f) != "forbidden" || !strings.Contains(f["msg"].(string), "stays on the laptop") {
			t.Errorf("%s: %v", a, f)
		}
	}
	if len(m.ai.conv.Messages) != 0 || len(m.ai.attachments) != 0 {
		t.Fatalf("sent %+v / %+v", m.ai.conv.Messages, m.ai.attachments)
	}
}

// Spec (MOB-5): "Explain why build failed" + a task attach gives the
// model the task's line (its command, last state) and the end of its
// terminal; a task the laptop does not list is not_found.
func TestChatSendAttachTask(t *testing.T) {
	m := webTestChatModel(t)
	webTestFakeAI(&m, "ok")
	m.output = newOutputStore()
	webTestHub(t, &m)
	webTestTasksRepo(t, &m)
	webTestFakeTasks(t)
	old := webTaskLines
	webTaskLines = func(_ *nvim.Client, buf, n int) ([]string, error) {
		return []string{"# example.com/x", "main.go:4: undefined: foo", "", ""}, nil
	}
	t.Cleanup(func() { webTaskLines = old })
	c := webTestSub(t, &m, "chat")
	webTestCall(t, &m, c, webproto.OpTasksRun, `{"label":"build"}`)
	f := webTestSendAttached(t, &m, c, `{"text":"Explain why build failed","attach":[{"kind":"task","id":"build"}]}`)
	if f["t"] != "res" {
		t.Fatalf("send: %v", f)
	}
	um := m.ai.conv.Messages[0]
	if !strings.Contains(um.Context, "go build ./...") || !strings.Contains(um.Context, "undefined: foo") {
		t.Fatalf("context %q", um.Context)
	}
	if len(um.Attached) != 1 || um.Attached[0].Kind != "task" || um.Attached[0].Label != "task build" {
		t.Fatalf("chips %+v", um.Attached)
	}
	m.ai.streaming = false
	if f := webTestSendAttached(t, &m, c, `{"text":"and this?","attach":[{"kind":"task","id":"deploy"}]}`); webTestErr(f) != "not_found" {
		t.Fatalf("unknown task: %v", f)
	}
}

// Spec (MOB-5): a file is cut at MaxAttachBytes, the message's attaches
// together at MaxAttachTotal; references (issue, view) go as short lines.
func TestChatSendAttachCaps(t *testing.T) {
	m, c, dir := webTestAttachModel(t)
	webTestWrite(t, dir, "big.txt", strings.Repeat("abcdefgh\n", 8000))
	list := strings.Repeat(`{"kind":"file","path":"big.txt"},`, 4) + `{"kind":"issue","id":"12"},{"kind":"view","id":"runs","spec":"{\"view\":\"runs\"}"}`
	f := webTestSendAttached(t, m, c, `{"text":"sum it up","attach":[`+list+`]}`)
	if f["t"] != "res" {
		t.Fatalf("send: %v", f)
	}
	um := m.ai.conv.Messages[0]
	if len(um.Attached) != 6 {
		t.Fatalf("chips %+v", um.Attached)
	}
	if n := len(um.Context); n > webproto.MaxAttachTotal+4096 {
		t.Fatalf("context %d bytes", n)
	}
	for _, want := range []string{"(cut)", "Issue 12", `"view":"runs"`} {
		if !strings.Contains(um.Context, want) {
			t.Errorf("context misses %q (%d bytes, ends %q)", want, len(um.Context), um.Context[max(0, len(um.Context)-300):])
		}
	}
}

// Spec (MOB-5): when the AI started meanwhile the send is busy and the
// attaches are not kept; a side question takes no attaches.
func TestChatSendAttachLaptopMovedOn(t *testing.T) {
	m, c, _ := webTestAttachModel(t)
	call, cmd := webTestCallLater(t, m, c, webproto.OpChatSend, `{"text":"x","attach":[{"kind":"file","path":"a.txt"}]}`)
	msg := cmd()
	m.ai.streaming = true
	m.handleWebMsg(msg)
	if f := webTestAnswer(t, c, call); webTestErr(f) != "busy" || len(m.ai.attachments) != 0 {
		t.Fatalf("busy: %v %+v", f, m.ai.attachments)
	}
	m.ai.streaming = false
	if f, _ := webTestCall(t, m, c, webproto.OpChatSide, `{"text":"q","attach":[{"kind":"diff"}]}`); webTestErr(f) != "bad_request" {
		t.Fatalf("side: %v", f)
	}
}
