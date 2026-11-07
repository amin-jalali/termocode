package canvas

import (
	"os"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func mustApply(t *testing.T, b *Board, op Op, by string) string {
	t.Helper()
	id, _, err := b.Apply(op, by, now, nil)
	if err != nil {
		t.Fatalf("%+v by %s: %v", op, by, err)
	}
	return id
}

// Spec: a missing file is an empty plan; Save writes .termocode/canvas.json
// and Load reads the same cards back, with the version bumped.
func TestCanvasLoadSaveRoundTrip(t *testing.T) {
	root := t.TempDir()
	b, err := Load(root)
	if err != nil || len(b.Cards) != 0 {
		t.Fatalf("empty root: %+v %v", b, err)
	}
	mustApply(t, &b, Op{Kind: OpAdd, Title: "write the parser", Files: []string{"internal/sql/parse.go"}}, ByYou)
	mustApply(t, &b, Op{Kind: OpAdd, Title: "add tests", Status: StatusDoing}, ByAI)
	if err := Save(root, &b); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(root)); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Cards) != 2 || got.Cards[0].ID != "c1" || got.Cards[1].ID != "c2" {
		t.Fatalf("read back %+v", got)
	}
	if !got.Cards[0].Approved || got.Cards[1].Approved || got.Cards[1].By != ByAI || got.Cards[1].Status != StatusDoing {
		t.Errorf("a user card is approved, an AI card waits: %+v", got.Cards)
	}
	os.WriteFile(Path(root), []byte("{not json"), 0o644)
	if _, err := Load(root); err == nil {
		t.Error("a broken file must be an error, not an empty plan")
	}
}

// Spec: the AI never approves a card, never removes one the user wrote
// or approved, and editing an approved card's text takes its OK away.
func TestCanvasAIRules(t *testing.T) {
	var b Board
	mine := mustApply(t, &b, Op{Kind: OpAdd, Title: "mine"}, ByYou)
	ai := mustApply(t, &b, Op{Kind: OpAdd, Title: "proposed"}, ByAI)
	if _, _, err := b.Apply(Op{Kind: OpApprove, ID: ai}, ByAI, now, nil); err == nil {
		t.Error("the AI approved a card")
	}
	if _, _, err := b.Apply(Op{Kind: OpRemove, ID: mine}, ByAI, now, nil); err == nil {
		t.Error("the AI removed the user's card")
	}
	mustApply(t, &b, Op{Kind: OpApprove, ID: ai}, ByYou)
	if _, _, err := b.Apply(Op{Kind: OpRemove, ID: ai}, ByAI, now, nil); err == nil {
		t.Error("the AI removed a card the user approved")
	}
	_, what, err := b.Apply(Op{Kind: OpEdit, ID: mine, Title: "mine, sharper"}, ByAI, now, nil)
	if err != nil || b.Cards[0].Approved || !strings.Contains(what, "OK again") {
		t.Errorf("edit by AI: %q %v approved=%v", what, err, b.Cards[0].Approved)
	}
	mustApply(t, &b, Op{Kind: OpStatus, ID: mine, Status: StatusDone}, ByAI)
	extra := mustApply(t, &b, Op{Kind: OpAdd, Title: "scratch"}, ByAI)
	mustApply(t, &b, Op{Kind: OpRemove, ID: extra}, ByAI)
	if b.Find(extra) >= 0 {
		t.Error("the AI could not remove its own unapproved card")
	}
	mustApply(t, &b, Op{Kind: OpRemove, ID: mine}, ByYou)
	if len(b.Cards) != 1 {
		t.Errorf("the user removes any card: %+v", b.Cards)
	}
}

// Spec: move puts a card at a 1-based place (clamped); ids stay.
func TestCanvasMove(t *testing.T) {
	var b Board
	for _, s := range []string{"a", "b", "c", "d"} {
		mustApply(t, &b, Op{Kind: OpAdd, Title: s}, ByYou)
	}
	mustApply(t, &b, Op{Kind: OpMove, ID: "c4", To: 1}, ByYou)
	mustApply(t, &b, Op{Kind: OpMove, ID: "c1", To: 99}, ByAI)
	var got []string
	for _, c := range b.Cards {
		got = append(got, c.ID)
	}
	if strings.Join(got, " ") != "c4 c2 c3 c1" {
		t.Errorf("order %v", got)
	}
	if id := mustApply(t, &b, Op{Kind: OpAdd, Title: "e"}, ByYou); id != "c5" {
		t.Errorf("next id %s", id)
	}
}

// Spec: titles are one clean line, cut to MaxTitle; secrets go through
// the redactor; files must stay inside the workspace.
func TestCanvasCleansInput(t *testing.T) {
	var b Board
	red := func(s string) string { return strings.ReplaceAll(s, "sk-secret", "[redacted]") }
	_, _, err := b.Apply(Op{Kind: OpAdd, Title: "use\nkey sk-secret\x1b[31m now " + strings.Repeat("x", 200)}, ByAI, now, red)
	if err != nil {
		t.Fatal(err)
	}
	c := b.Cards[0]
	if strings.ContainsAny(c.Title, "\n\x1b") || strings.Contains(c.Title, "sk-secret") || len([]rune(c.Title)) > MaxTitle {
		t.Errorf("title not cleaned: %q", c.Title)
	}
	for _, bad := range []string{"/etc/passwd", "../outside.go"} {
		if _, _, err := b.Apply(Op{Kind: OpAdd, Title: "x", Files: []string{bad}}, ByAI, now, nil); err == nil {
			t.Errorf("file %q was taken", bad)
		}
	}
	if _, _, err := b.Apply(Op{Kind: OpAdd, Title: "  "}, ByYou, now, nil); err == nil {
		t.Error("a card without a title was added")
	}
	if _, _, err := b.Apply(Op{Kind: OpStatus, ID: "c1", Status: "later"}, ByYou, now, nil); err == nil {
		t.Error("a bad status was taken")
	}
}

// Spec: Text (for the AI) and Markdown (copy) list every card in order
// with its status, author and OK.
func TestCanvasTextAndMarkdown(t *testing.T) {
	var b Board
	mustApply(t, &b, Op{Kind: OpAdd, Title: "plan the schema", Note: "tables first"}, ByYou)
	mustApply(t, &b, Op{Kind: OpAdd, Title: "write migrations", Status: StatusDone}, ByAI)
	txt := b.Text()
	for _, want := range []string{"1 to do, 0 doing, 1 done; 1 wait", "1. c1 [to do] plan the schema (by the user, approved)",
		"   tables first", "2. c2 [done] write migrations (by AI, needs the user's OK)"} {
		if !strings.Contains(txt, want) {
			t.Errorf("Text has no %q:\n%s", want, txt)
		}
	}
	md := b.Markdown()
	for _, want := range []string{"# Shared plan", "- [ ] plan the schema\n", "  tables first", "- [x] write migrations _(by AI, needs OK)_"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown has no %q:\n%s", want, md)
		}
	}
}
