package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (AGX-16): the quick model's reply becomes at most 3 clean slugs:
// lower case, a-z 0-9 and -, list marks and quotes gone, duplicates and
// empty lines dropped, 40 cells at most.
func TestAgentNameSlugs(t *testing.T) {
	got := agentNameSlugs("1. Fix-Toast_Queue!\n- `make toast thread safe`\n* fix toast queue\n\n\"queue\"\nfourth-one\n")
	want := []string{"fix-toast-queue", "make-toast-thread-safe", "queue"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("slugs %q, want %q", got, want)
	}
	long := agentNameSlugs(strings.Repeat("word-", 30))
	if len(long) != 1 || len(long[0]) > 40 || strings.HasSuffix(long[0], "-") {
		t.Errorf("long %q", long)
	}
	if s := agentNameSlugs("--\n!!!\n"); len(s) != 0 {
		t.Errorf("junk %q", s)
	}
}

// Spec (AGX-16): the request holds the task title only, capped and with
// secrets hidden, and asks for 3 slugs.
func TestAgentNameRequest(t *testing.T) {
	secret := "sk-" + "test0123456789abcdefghijklmnop"
	req := agentNameRequest("quick-1", "Fix the toast queue "+secret+" "+strings.Repeat("x", 600))
	if len(req.Messages) != 2 || req.Model != "quick-1" {
		t.Fatalf("req %+v", req)
	}
	user := req.Messages[1].Content
	if strings.Contains(user, secret) || len([]rune(user)) > agentNameTaskMax+40 || !strings.Contains(user, "Fix the toast queue") {
		t.Errorf("user %q", user)
	}
	if !strings.Contains(req.Messages[0].Content, "3") {
		t.Errorf("system %q", req.Messages[0].Content)
	}
}

// Spec (AGX-16): without a model no request is sent: the row says to
// set a quick model. The AI cannot open the picker.
func TestAgentNameNoModel(t *testing.T) {
	m := aiTestModel(t)
	if p, ok := aiActionPolicyFor(agentNamesPaletteID); !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
	found := false
	for _, it := range tasksPaletteItems() {
		if it.ID == agentNamesPaletteID && it.Title == "Worktree: Suggest a Name…" {
			found = true
		}
	}
	if !found {
		t.Error("no palette row")
	}
	m.dispatchTasksPalette(agentNamesPaletteID)
	if !m.promptOpen || m.promptKind != promptKindAgentNames {
		t.Fatalf("prompt %v %v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	if cmd := m.onAgentNamesTask("Fix the toast queue"); cmd != nil {
		t.Error("a request without a model")
	}
	if !m.pickerOpen || m.pickerKind != pickerKindAgentNames || !strings.Contains(ansi.Strip(m.picker.View()), "set a quick model") {
		t.Errorf("picker %v %v:\n%s", m.pickerOpen, m.pickerKind, ansi.Strip(m.picker.View()))
	}
}

// Spec (AGX-16): with a quick model the task goes off the UI thread; the
// picker lists the slugs with the cost; Enter copies the picked one.
func TestAgentNameSuggestFlow(t *testing.T) {
	m := aiTestModel(t)
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: "fix-toast-queue\nthread-safe-toasts\ntoast-mutex\nmore",
		Usage: &ai.Usage{Model: "quick-1", Input: 120, Output: 12, Requests: 1}}}}
	m.ai.slotFake = map[string]aiSlot{ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOllama, Model: "quick-1"}}}
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	cmd := m.onAgentNamesTask("Make the toast queue thread-safe")
	if cmd == nil {
		t.Fatal("no request")
	}
	runApply(&m, cmd)
	if len(quick.reqs) != 1 {
		t.Fatalf("requests %d", len(quick.reqs))
	}
	view := ansi.Strip(m.picker.View())
	for _, w := range []string{"fix-toast-queue", "thread-safe-toasts", "toast-mutex", "!more", "quick-1"} {
		if strings.HasPrefix(w, "!") {
			if strings.Contains(view, w[1:]) {
				t.Errorf("%q shows", w[1:])
			}
			continue
		}
		if !strings.Contains(view, w) {
			t.Errorf("no %q in\n%s", w, view)
		}
	}
	m.pickerOpen = false
	m.onAgentNamePicked(agentNamePfx + "thread-safe-toasts")
	if len(copied) != 1 || copied[0] != "thread-safe-toasts" {
		t.Errorf("copied %q", copied)
	}
	if !strings.Contains(toastText(m), "thread-safe-toasts") {
		t.Errorf("toast %q", toastText(m))
	}
}
