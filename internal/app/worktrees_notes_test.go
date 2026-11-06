package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtCtl sends one control request to the UI and runs what it starts,
// as the program would, until the reply comes.
func wtCtl(t *testing.T, m *Model, req agentctl.Request) agentctl.Reply {
	t.Helper()
	call := agentCtlCall{req: req, reply: make(chan agentctl.Reply, 1)}
	wtLinkDrive(t, m, m.onAgentCtl(call))
	select {
	case r := <-call.reply:
		return r
	case <-time.After(10 * time.Second):
		t.Fatalf("no reply to %+v", req)
	}
	return agentctl.Reply{}
}

func wtRepoFeat(t *testing.T) (root, feat, common string) {
	t.Helper()
	root = wtRepo(t)
	list, common, err := git.ListWorktrees(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range list {
		if w.Branch == "feat/x" {
			feat = w.Path
		}
	}
	return root, feat, common
}

// WTX-12: an agent's note and status land on the worktree its folder is
// in: one clean, redacted line with its time; an unknown folder, an
// empty or long note and a bad status are refused.
func TestWorktreeCtlNoteAndStatus(t *testing.T) {
	withConfigDir(t)
	root, feat, common := wtRepoFeat(t)
	m := wtTestModel(t, root)
	tok := "ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789"
	if r := wtCtl(t, m, agentctl.Request{Op: agentctl.OpWorktreeNote, Dir: feat + "/sub", Text: "pushed with " + tok + "\nnext: tests"}); !r.OK {
		t.Fatalf("note: %+v", r)
	}
	if r := wtCtl(t, m, agentctl.Request{Op: agentctl.OpWorktreeStatus, Dir: feat, State: "blocked"}); !r.OK {
		t.Fatalf("status: %+v", r)
	}
	md, _ := worktrees.Load(common)
	got := md[feat]
	if len(got.Notes) != 1 || strings.Contains(got.Notes[0].Text, tok) || strings.Contains(got.Notes[0].Text, "\n") || !strings.Contains(got.Notes[0].Text, "next: tests") || got.Notes[0].At.IsZero() {
		t.Errorf("note %+v", got.Notes)
	}
	if got.Checkpoint != "blocked" {
		t.Errorf("checkpoint %q", got.Checkpoint)
	}
	for _, bad := range []agentctl.Request{
		{Op: agentctl.OpWorktreeNote, Dir: t.TempDir(), Text: "x"},
		{Op: agentctl.OpWorktreeNote, Dir: feat, Text: "  "},
		{Op: agentctl.OpWorktreeNote, Dir: feat, Text: strings.Repeat("a", worktrees.MaxNoteLen+1)},
		{Op: agentctl.OpWorktreeStatus, Dir: feat, State: "waiting"},
	} {
		if r := wtCtl(t, m, bad); r.OK || r.Error == "" {
			t.Errorf("%+v accepted: %+v", bad, r)
		}
	}
	r := wtCtl(t, m, agentctl.Request{Op: agentctl.OpWorktreeList, Dir: root})
	found := false
	for _, w := range r.Worktrees {
		if w.Path == feat && w.Checkpoint == "blocked" && strings.Contains(w.Note, "next: tests") {
			found = true
		}
	}
	if !r.OK || !found {
		t.Errorf("list %+v", r)
	}
}

// WTX-9 / WTX-12: a link from the CLI asks first (Deny first and
// focused); Deny links nothing; Link reads it with gh and stores it.
func TestWorktreeCtlLinkAsks(t *testing.T) {
	withConfigDir(t)
	wtFakeGHSeam(t, map[string]string{"issue 412": `{"number":412,"title":"Pinned row","state":"OPEN","url":"u"}`})
	root, feat, common := wtRepoFeat(t)
	m := wtTestModel(t, root)
	answer := func(id string) agentctl.Reply {
		call := agentCtlCall{req: agentctl.Request{Op: agentctl.OpWorktreeLink, Dir: feat, Text: "https://github.com/o/r/issues/412"}, reply: make(chan agentctl.Reply, 1)}
		wtLinkDrive(t, m, m.onAgentCtl(call))
		if !m.confirmOpen || m.confirmKind != confirmKindWTCtlLink || m.confirm.SafeID() != "deny" {
			t.Fatalf("no Deny-first card: %v %d %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
		}
		if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "#412") || !strings.Contains(box, "feat/x") {
			t.Errorf("card:\n%s", box)
		}
		m.confirmOpen = false
		cmd, _ := m.handleAIConfirm(id)
		wtLinkDrive(t, m, cmd)
		select {
		case r := <-call.reply:
			return r
		case <-time.After(5 * time.Second):
			t.Fatal("no reply")
		}
		return agentctl.Reply{}
	}
	if r := answer("deny"); r.OK {
		t.Errorf("deny linked: %+v", r)
	}
	if md, _ := worktrees.Load(common); md[feat].Issue != 0 {
		t.Fatalf("deny stored %+v", md[feat])
	}
	if r := answer("link"); !r.OK {
		t.Errorf("link: %+v", r)
	}
	if md, _ := worktrees.Load(common); md[feat].Issue != 412 || md[feat].Link.Title != "Pinned row" {
		t.Errorf("stored %+v", md[feat])
	}
}

// WTX-12: the AI tools: worktrees_list (read, with links, status and the
// last note) and worktree_note (auto: text only, redacted); both reach
// outside agents over MCP; neither may set a link.
func TestWorktreeAITools(t *testing.T) {
	withConfigDir(t)
	root, feat, common := wtRepoFeat(t)
	tools := aiWorktreeTools(root)
	note, ok := ai.FindTool(tools, "worktree_note")
	if !ok || note.Mutating {
		t.Fatalf("worktree_note %+v", note)
	}
	list, ok := ai.FindTool(tools, "worktrees_list")
	if !ok || list.Mutating {
		t.Fatalf("worktrees_list %+v", list)
	}
	if _, ok := note.Params["issue"]; ok {
		t.Error("worktree_note takes a link")
	}
	if _, err := note.Run(context.Background(), map[string]any{"worktree": "feat/x", "text": "tests pass", "status": "done"}); err != nil {
		t.Fatal(err)
	}
	md, _ := worktrees.Load(common)
	if n, _ := md[feat].LastNote(); n.Text != "tests pass" || md[feat].Checkpoint != "done" {
		t.Errorf("meta %+v", md[feat])
	}
	if _, err := note.Run(context.Background(), map[string]any{"worktree": "/nowhere", "text": "x"}); err == nil {
		t.Error("an unknown worktree took a note")
	}
	out, err := list.Run(context.Background(), map[string]any{})
	if err != nil || !strings.Contains(out, "feat/x") || !strings.Contains(out, "tests pass") || !strings.Contains(out, "done") {
		t.Errorf("list %v:\n%s", err, out)
	}
	for _, n := range []string{"worktrees_list", "worktree_note"} {
		if _, ok := mcpServeRow(n); !ok {
			t.Errorf("%s not offered to outside agents", n)
		}
	}
}

// WTX-12: N lists the selected worktree's notes with their times, newest
// first; the card line 3 shows the last note and line 4 the status.
func TestWorktreeNotesCard(t *testing.T) {
	st, _ := wtFixture(t)
	m := &Model{wt: st, focus: FocusExplorer, explorerWidth: 40, h: 42, w: 150}
	for i := range st.entries {
		if st.entries[i].Path == "/src/app.worktrees/toast" {
			st.entries[i].Meta.Notes = []worktrees.Note{{Text: "first", At: wtTestNow.Add(-time.Hour)}, {Text: "second", At: wtTestNow.Add(-time.Minute)}}
			st.entries[i].Meta.Checkpoint = "blocked"
		}
	}
	if c := wtCardText(m, "/src/app.worktrees/toast", 40); !strings.Contains(c, `"second"`) || !strings.Contains(c, "blocked") {
		t.Errorf("card:\n%s", c)
	}
	st.cursorPath = "/src/app.worktrees/toast"
	wtKey(t, m, "N")
	if !m.confirmOpen {
		t.Fatal("no notes card")
	}
	box := ansi.Strip(m.confirm.Box())
	if i, j := strings.Index(box, "second"), strings.Index(box, "first"); i < 0 || j < 0 || i > j {
		t.Errorf("notes card:\n%s", box)
	}
	m.confirmOpen = false
	st.cursorPath = "/src/app"
	wtKey(t, m, "N")
	if m.confirmOpen || !strings.Contains(wtLastToast(m), "No notes") {
		t.Errorf("empty: open %v toast %q", m.confirmOpen, wtLastToast(m))
	}
}
