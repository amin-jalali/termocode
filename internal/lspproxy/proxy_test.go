package lspproxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestParseMap(t *testing.T) {
	m, err := ParseMap("/app/=/home/u/proj")
	if err != nil || m.Remote != "/app" || m.Local != "/home/u/proj" {
		t.Fatalf("ParseMap = %+v, %v", m, err)
	}
	for _, bad := range []string{"", "/app", "=/x", "app=/x", "/app=x"} {
		if _, err := ParseMap(bad); err == nil {
			t.Errorf("ParseMap(%q) gave no error", bad)
		}
	}
}

func TestRewriteInitializeToServer(t *testing.T) {
	m := Map{Remote: "/app", Local: "/home/u/proj"}
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":42,"rootPath":"/app","rootUri":"file:///app","workspaceFolders":[{"uri":"file:///app","name":"app"}]}}`
	out := m.ToServer().Rewrite([]byte(in))
	var v struct {
		ID     json.Number
		Params struct {
			ProcessID        int
			RootPath         string
			RootURI          string
			WorkspaceFolders []struct{ URI, Name string }
		}
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	if v.ID != "1" || v.Params.ProcessID != 42 {
		t.Errorf("numbers changed: %s", out)
	}
	if v.Params.RootPath != "/home/u/proj" || v.Params.RootURI != "file:///home/u/proj" {
		t.Errorf("root not rewritten: %s", out)
	}
	if len(v.Params.WorkspaceFolders) != 1 || v.Params.WorkspaceFolders[0].URI != "file:///home/u/proj" || v.Params.WorkspaceFolders[0].Name != "app" {
		t.Errorf("workspace folder: %s", out)
	}
}

func TestRewriteToClientKeepsOutsidePaths(t *testing.T) {
	m := Map{Remote: "/app", Local: "/home/u/proj"}
	in := `{"jsonrpc":"2.0","id":3,"result":[{"uri":"file:///home/u/proj/a%20b.go","range":{}},{"uri":"file:///home/u/go/pkg/mod/x.go"},{"uri":"file:///home/u/project/x.go"}]}`
	out := string(m.ToClient().Rewrite([]byte(in)))
	if !strings.Contains(out, `"file:///app/a%20b.go"`) {
		t.Errorf("own file not mapped back: %s", out)
	}
	if !strings.Contains(out, `"file:///home/u/go/pkg/mod/x.go"`) {
		t.Errorf("a module cache file must stay local: %s", out)
	}
	if !strings.Contains(out, `"file:///home/u/project/x.go"`) {
		t.Errorf("a sibling folder with the same prefix must not match: %s", out)
	}
}

func TestRewriteWorkspaceEditKeys(t *testing.T) {
	m := Map{Remote: "/app", Local: "/home/u/proj"}
	in := `{"id":9,"method":"workspace/applyEdit","params":{"edit":{"changes":{"file:///home/u/proj/main.go":[{"newText":"x"}]}}}}`
	out := string(m.ToClient().Rewrite([]byte(in)))
	if !strings.Contains(out, `"file:///app/main.go"`) || strings.Contains(out, "/home/u/proj") {
		t.Errorf("changes key not rewritten: %s", out)
	}
}

func TestRewriteLeavesOtherMessagesAlone(t *testing.T) {
	m := Map{Remote: "/app", Local: "/home/u/proj"}
	for _, in := range []string{`{"id":1,"result":null}`, `not json /app`, `{"text":"see /app/x"}`} {
		if got := string(m.ToServer().Rewrite([]byte(in))); got != in {
			t.Errorf("Rewrite(%s) = %s", in, got)
		}
	}
}

func TestFramingRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	for _, b := range []string{`{"a":1}`, `{"b":"ü"}`} {
		if err := WriteMessage(&buf, []byte(b)); err != nil {
			t.Fatal(err)
		}
	}
	br := bufio.NewReader(&buf)
	for _, want := range []string{`{"a":1}`, `{"b":"ü"}`} {
		got, err := ReadMessage(br)
		if err != nil || string(got) != want {
			t.Fatalf("ReadMessage = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := ReadMessage(br); err != io.EOF {
		t.Errorf("end of stream = %v, want io.EOF", err)
	}
}

func TestReadMessageRejectsBadLength(t *testing.T) {
	for _, in := range []string{"Content-Type: x\r\n\r\n{}", "Content-Length: 999999999999\r\n\r\n"} {
		if _, err := ReadMessage(bufio.NewReader(strings.NewReader(in))); err == nil {
			t.Errorf("ReadMessage(%q) gave no error", in)
		}
	}
}

func TestPumpRewritesEveryMessage(t *testing.T) {
	var src, dst bytes.Buffer
	_ = WriteMessage(&src, []byte(`{"params":{"textDocument":{"uri":"file:///app/main.go"}}}`))
	_ = WriteMessage(&src, []byte(`{"id":2,"result":null}`))
	if err := Pump(&dst, &src, Rewriter{From: "/app", To: "/m/x"}); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(&dst)
	first, _ := ReadMessage(br)
	if !strings.Contains(string(first), "file:///m/x/main.go") {
		t.Errorf("first = %s", first)
	}
	second, _ := ReadMessage(br)
	if string(second) != `{"id":2,"result":null}` {
		t.Errorf("second = %s", second)
	}
}
