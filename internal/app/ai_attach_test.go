package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiAttachTree makes a workspace with a few files for the path tests.
func aiAttachTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"a.go":               "package a\n",
		"my file.txt":        "spaces\n",
		"dir/b.md":           "# b\n",
		"it's.txt":           "quote\n",
		"pic.png":            "\x89PNG\r\n\x1a\nfake",
		"blob.bin":           "ab\x00cd",
		"dir/with space.txt": "x\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestAIDroppedPaths(t *testing.T) {
	root := aiAttachTree(t)
	j := func(n string) string { return filepath.Join(root, n) }
	cases := []struct {
		name  string
		paste string
		want  []string // nil: plain text
	}{
		{"absolute", j("a.go"), []string{j("a.go")}},
		{"relative", "a.go", []string{j("a.go")}},
		{"trailing newline", j("a.go") + "\n", []string{j("a.go")}},
		{"single quotes", "'" + j("my file.txt") + "' ", []string{j("my file.txt")}},
		{"double quotes", `"` + j("my file.txt") + `"`, []string{j("my file.txt")}},
		{"escaped spaces", strings.ReplaceAll(j("my file.txt"), " ", `\ `), []string{j("my file.txt")}},
		{"raw spaces", j("my file.txt"), []string{j("my file.txt")}},
		{"file uri", "file://" + strings.ReplaceAll(j("my file.txt"), " ", "%20"), []string{j("my file.txt")}},
		{"file uri localhost", "file://localhost" + j("a.go"), []string{j("a.go")}},
		{"shell quote inside", `'` + strings.ReplaceAll(j("it's.txt"), "'", `'\''`) + `'`, []string{j("it's.txt")}},
		{"several, spaces", j("a.go") + " " + strings.ReplaceAll(j("dir/with space.txt"), " ", `\ `), []string{j("a.go"), j("dir/with space.txt")}},
		{"several, quoted", "'" + j("a.go") + "' '" + j("dir/b.md") + "'", []string{j("a.go"), j("dir/b.md")}},
		{"several, lines", j("a.go") + "\n" + j("dir/b.md") + "\n", []string{j("a.go"), j("dir/b.md")}},
		{"several, uris", "file://" + j("a.go") + "\r\nfile://" + j("dir/b.md"), []string{j("a.go"), j("dir/b.md")}},
		{"missing file", j("nope.go"), nil},
		{"one missing of two", j("a.go") + " " + j("nope.go"), nil},
		{"folder", j("dir"), nil},
		{"plain text", "hello world", nil},
		{"code", "func main() {}", nil},
		{"empty", "  \n", nil},
	}
	for _, c := range cases {
		got, ok := aiDroppedPaths(c.paste, root)
		if c.want == nil {
			if ok {
				t.Errorf("%s: %q should stay text, got %v", c.name, c.paste, got)
			}
			continue
		}
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %q → %v %v, want %v", c.name, c.paste, got, ok, c.want)
		}
	}
}

func TestAILoadAttachment(t *testing.T) {
	root := aiAttachTree(t)
	at, err := aiLoadAttachment(filepath.Join(root, "a.go"), root, true)
	if err != nil || at.Label != "a.go" || at.Text != "package a\n" || at.Image != "" {
		t.Errorf("text: %+v %v", at, err)
	}
	at, err = aiLoadAttachment(filepath.Join(root, "dir/b.md"), root, true)
	if err != nil || at.Label != "dir/b.md" {
		t.Errorf("label is workspace-relative: %+v %v", at, err)
	}
	at, err = aiLoadAttachment(filepath.Join(root, "pic.png"), root, true)
	if err != nil || at.Image != filepath.Join(root, "pic.png") || at.MediaType != "image/png" || at.Text != "" {
		t.Errorf("image: %+v %v", at, err)
	}
	at, err = aiLoadAttachment(filepath.Join(root, "pic.png"), root, false)
	if err != nil || at.Image != "" || !strings.Contains(at.Text, "not sent: provider has no image input") {
		t.Errorf("image without image input: %+v %v", at, err)
	}
	if _, err = aiLoadAttachment(filepath.Join(root, "blob.bin"), root, true); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Errorf("binary: want refusal, got %v", err)
	}
	big := filepath.Join(root, "big.txt")
	_ = os.WriteFile(big, []byte(strings.Repeat("x", aiMaxAttach+10)), 0o644)
	at, err = aiLoadAttachment(big, root, true)
	if err != nil || len(at.Text) > aiMaxAttach+40 || !strings.HasSuffix(at.Text, "[truncated]") {
		t.Errorf("size cap: len %d %v", len(at.Text), err)
	}
}

// pasteKey is a bracketed paste as Bubble Tea delivers it.
func pasteKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

// Pasting a path into the focused chat input makes a chip; other text
// is typed. Backspace on an empty input removes the last chip; a chip's
// × removes that chip.
func TestAIPastePathMakesChip(t *testing.T) {
	m := aiTestModel(t)
	root := aiAttachTree(t)
	t.Chdir(root)
	s := m.ai
	s.provider = fakeAIProvider{reply: "ok"}
	m.actionsOpen, m.actionsPinned = true, true
	s.focused = true

	if _, ok := m.routeAIPanelKey(pasteKey(filepath.Join(root, "a.go"))); !ok {
		t.Fatal("paste not handled")
	}
	if len(s.attachments) != 1 || s.attachments[0].Label != "a.go" || len(s.input) != 0 {
		t.Fatalf("want one chip, no text: %+v %q", s.attachments, string(s.input))
	}
	m.routeAIPanelKey(pasteKey("hello there"))
	if string(s.input) != "hello there" || len(s.attachments) != 1 {
		t.Fatalf("plain paste: %q %+v", string(s.input), s.attachments)
	}
	m.routeAIPanelKey(pasteKey(filepath.Join(root, "blob.bin")))
	if len(s.attachments) != 1 || m.toast.Empty() {
		t.Errorf("binary must be refused with a toast: %+v", s.attachments)
	}
	m.routeAIPanelKey(pasteKey(filepath.Join(root, "dir/b.md")))
	if len(s.attachments) != 2 {
		t.Fatalf("second chip: %+v", s.attachments)
	}
	// × on the first chip.
	var rm string
	for _, b := range m.aiDividerLine(60).buttons {
		if strings.HasPrefix(b.id, "rm-attach:") {
			rm = b.id
			break
		}
	}
	if rm != "rm-attach:0" {
		t.Fatalf("no × button on the chips: %q", rm)
	}
	m.onAIButton(rm)
	if len(s.attachments) != 1 || s.attachments[0].Label != "dir/b.md" {
		t.Fatalf("× removes that chip: %+v", s.attachments)
	}
	// Backspace with text deletes text; on an empty input it drops a chip.
	s.input, s.caret = nil, 0
	m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(s.attachments) != 0 {
		t.Fatalf("backspace on empty input keeps the chip: %+v", s.attachments)
	}
}

// "@path" in the message attaches that workspace file on send.
func TestAIAtMentionAttaches(t *testing.T) {
	root := aiAttachTree(t)
	t.Chdir(root)
	m := aiTestModel(t)
	got := aiMentionedPaths("look at @a.go and @file dir/b.md, not @nope.go or me@example.com", root)
	want := []string{filepath.Join(root, "a.go"), filepath.Join(root, "dir/b.md")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mentions: %v want %v", got, want)
	}
	s := m.ai
	s.provider = fakeAIProvider{reply: "ok"}
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake"}
	s.input = []rune("explain @a.go")
	s.caret = len(s.input)
	cmd := m.submitAIInput()
	if cmd == nil || len(s.conv.Messages) == 0 {
		t.Fatal("not sent")
	}
	if ctx := s.conv.Messages[0].Context; !strings.Contains(ctx, "Attached a.go") || !strings.Contains(ctx, "package a") {
		t.Errorf("@a.go not attached: %q", ctx)
	}
	s.chatCancel()
}

// Images go to the provider as Message.Images, rebuilt from the stored
// path on every turn.
func TestAIImageAttachmentInHistory(t *testing.T) {
	root := aiAttachTree(t)
	m := aiTestModel(t)
	s := m.ai
	s.provider = ai.NewProvider(ai.Config{Kind: ai.KindAnthropic, APIKey: "k-12345678"})
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "what is it", Images: []string{filepath.Join(root, "pic.png"), filepath.Join(root, "gone.png")}}}
	msgs := m.aiChatHistory("sys")
	if len(msgs) != 2 || len(msgs[1].Images) != 1 || msgs[1].Images[0].MediaType != "image/png" {
		t.Fatalf("history images: %+v", msgs)
	}
	if !strings.Contains(msgs[1].Content, "gone.png") {
		t.Errorf("a missing image is named in the text: %q", msgs[1].Content)
	}
}

func TestAISaveAsDefaultName(t *testing.T) {
	cases := []struct{ text, want string }{
		{"Here:\n```go\npackage main\n```", "snippet.go"},
		{"Put this in `cmd/main.go`:\n\n```go\npackage main\n```", "cmd/main.go"},
		{"```python title=tools/run.py\nprint(1)\n```", "tools/run.py"},
		{"```\nhello\n```", "snippet.txt"},
		{"```sh\nls\n```", "snippet.sh"},
	}
	for _, c := range cases {
		_, lang, hint, ok := aiCodeBlockInfo(c.text, 0)
		if !ok {
			t.Fatalf("no block in %q", c.text)
		}
		if got := aiSaveAsName(lang, hint); got != c.want {
			t.Errorf("%q → %q, want %q", c.text, got, c.want)
		}
	}
}

// "Save as…" refuses paths outside the workspace.
func TestAISaveAsStaysInWorkspace(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"../x.go", "/etc/passwd", ".", "a/../../x.go"} {
		if p, ok := aiInWorkspace(bad, root); ok {
			t.Errorf("%q accepted as %q", bad, p)
		}
	}
	for in, want := range map[string]string{"x.go": "x.go", "a/b.go": "a/b.go", filepath.Join(root, "c.go"): "c.go"} {
		if p, ok := aiInWorkspace(in, root); !ok || p != filepath.Join(root, want) {
			t.Errorf("%q → %q %v", in, p, ok)
		}
	}
}

func TestAIChatMarkdown(t *testing.T) {
	c := ai.Conversation{Title: "Fix it", Messages: []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "hi", Context: "Attached a.go:\n```\npackage a\n```"},
		{Role: ai.StoredTool, Text: "⚙ Read a.go → ok"},
		{Role: ai.StoredAssistant, Text: "Hello **there**"},
		{Role: ai.StoredError, Text: "boom"},
	}}
	md := aiChatMarkdown(c)
	for _, want := range []string{"# Fix it", "## You", "hi", "a.go", "## AI", "Hello **there**", "⚙ Read a.go", "boom"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown misses %q:\n%s", want, md)
		}
	}
}
