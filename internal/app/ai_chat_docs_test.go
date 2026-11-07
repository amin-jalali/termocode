package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/chatdocs"
)

func TestChatDocToolsWriteReadList(t *testing.T) {
	st := chatdocs.Store{Dir: t.TempDir()}
	tools := aiChatDocTools(st)
	for _, n := range []string{"chat_doc_list", "chat_doc_read", "chat_doc_write"} {
		if _, ok := ai.FindTool(tools, n); !ok {
			t.Fatalf("no tool %s", n)
		}
	}
	write, _ := ai.FindTool(tools, "chat_doc_write")
	if _, err := write.Run(context.Background(), map[string]any{"name": "Plan", "text": "# Plan\n- step"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write.Run(context.Background(), map[string]any{"name": "", "text": "x"}); err == nil {
		t.Fatal("a doc with no name was written")
	}
	read, _ := ai.FindTool(tools, "chat_doc_read")
	out, err := read.Run(context.Background(), map[string]any{"name": "plan"})
	if err != nil || !strings.Contains(out, "- step") {
		t.Fatalf("read %q %v", out, err)
	}
	list, _ := ai.FindTool(tools, "chat_doc_list")
	out, err = list.Run(context.Background(), nil)
	if err != nil || !strings.Contains(out, "plan") || !strings.Contains(out, "1 revision") {
		t.Fatalf("list %q %v", out, err)
	}
}

func TestChatDocReadHidesSecrets(t *testing.T) {
	st := chatdocs.Store{Dir: t.TempDir()}
	_, _ = st.Write("keys", "token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB", chatdocs.AuthorYou)
	read, _ := ai.FindTool(aiChatDocTools(st), "chat_doc_read")
	out, err := read.Run(context.Background(), map[string]any{"name": "keys"})
	if err != nil || strings.Contains(out, "ghp_0123456789") {
		t.Fatalf("a secret reached the model: %q %v", out, err)
	}
}

func TestChatDocRestoreAsksCancelFirst(t *testing.T) {
	withConfigDir(t)
	m := &Model{w: 120, h: 40}
	st := chatdocs.Store{Dir: t.TempDir()}
	_, _ = st.Write("plan", "one", chatdocs.AuthorAI)
	_, _ = st.Write("plan", "two", chatdocs.AuthorAI)
	m.chatDocRestoreAsk(st, "plan", 1)
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	bs := m.confirm.Buttons()
	if len(bs) < 2 || bs[0].ID != "cancel" {
		t.Fatalf("buttons %+v: Cancel must come first", bs)
	}
	if txt, _ := st.Read("plan"); txt != "two" {
		t.Fatal("the card changed the doc before Restore")
	}
	_ = m.gitx.onConfirm(m, "restore")
	if txt, _ := st.Read("plan"); txt != "one" {
		t.Fatalf("Restore did not bring rev 1 back: %q", txt)
	}
}
