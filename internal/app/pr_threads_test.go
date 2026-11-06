package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const prThreadsText14 = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
 {"id":"PRRT_2","isResolved":true,"isOutdated":false,"path":"docs/a.md","line":3,"diffSide":"RIGHT",
  "comments":{"nodes":[{"databaseId":600,"author":{"login":"cy"},"body":"typo","createdAt":"2026-10-27T09:00:00Z","diffHunk":"","url":"u600"}]}},
 {"id":"PRRT_1","isResolved":false,"isOutdated":false,"path":"parser.go","line":12,"diffSide":"RIGHT",
  "comments":{"nodes":[
   {"databaseId":501,"author":{"login":"bob"},"body":"Why a map?\n` + "```suggestion\\nvar seen []string\\n```" + `","createdAt":"2026-10-28T10:00:00Z","diffHunk":"@@ -1,2 +1,2 @@\n package p\n-var seen = map[string]bool{}\n+var seen = map[string]int{}","url":"https://github.com/o/r/pull/14#discussion_r501"},
   {"databaseId":502,"author":{"login":"ann"},"body":"Faster lookups.","createdAt":"2026-10-28T11:00:00Z","diffHunk":"","url":"u502"}]}}
]}}}}}`

// prThreadsModel has PR 14 open with two review threads.
func prThreadsModel(t *testing.T) (*Model, *prFakeGH) {
	t.Helper()
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14, "api graphql": prThreadsText14, "api --method": "{}"}}
	m := prModel(f)
	prDo(t, m, m.prViewCmd(14))
	return m, f
}

// The view lists the review threads (open first) and has a Threads…
// button; a PR without threads (gh answered nothing) has none.
func TestPRViewShowsThreads(t *testing.T) {
	m, f := prThreadsModel(t)
	if !f.called("api graphql") {
		t.Fatalf("threads not read: %v", f.calls)
	}
	body := m.prViewBody()
	for _, want := range []string{"Review threads (1 open · 1 resolved)", "○ parser.go:12 · bob: Why a map?", "✓ docs/a.md:3 · cy: typo",
		"Files… then c comments on a change, s suggests a fix"} {
		if !strings.Contains(body, want) {
			t.Errorf("view misses %q:\n%s", want, body)
		}
	}
	if strings.Index(body, "parser.go:12") > strings.Index(body, "docs/a.md:3") {
		t.Error("the open thread is not first")
	}
	ids := []string{}
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	if strings.Join(ids, " ") != "actions threads files close" {
		t.Errorf("buttons %v", ids)
	}

	m2, _ := prViewModel(t) // no "api graphql" answer: no threads, no error
	for _, b := range m2.confirm.Buttons() {
		if b.ID == "threads" {
			t.Error("Threads… without threads")
		}
	}
	if toastHas(m2, "Could not") {
		t.Error("a missing thread list made an error toast")
	}
}

// Threads… lists the threads; Enter opens one thread with its code,
// every comment and the suggested change; Close is first.
func TestPRThreadCard(t *testing.T) {
	m, _ := prThreadsModel(t)
	if cmd, ok := m.handlePRConfirm("threads"); !ok || cmd != nil || !m.pickerOpen || m.pickerKind != pickerKindPRThreads {
		t.Fatalf("threads picker not open (%v %v)", m.pickerOpen, m.pickerKind)
	}
	box := ansi.Strip(m.picker.Box())
	for _, want := range []string{"#14 Review Threads", "parser.go:12", "2 comments"} {
		if !strings.Contains(box, want) {
			t.Errorf("picker misses %q:\n%s", want, box)
		}
	}
	m.pickerOpen = false
	m.handlePRPickerSelect("thread-PRRT_1")
	if !m.confirmOpen || m.confirmKind != confirmKindPRThread {
		t.Fatal("thread card not open")
	}
	text := prThreadText(*m.prS().threadOn())
	for _, want := range []string{"parser.go:12 · Open", "+var seen = map[string]int{}", "bob", "Why a map?",
		"Suggested change", "+ var seen []string", "ann", "Faster lookups."} {
		if !strings.Contains(text, want) {
			t.Errorf("card misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "```") {
		t.Errorf("the raw fence is shown:\n%s", text)
	}
	b := m.confirm.Buttons()
	if len(b) != 4 || b[0].ID != "close" || b[1].ID != "reply" || b[2].ID != "resolve" || b[3].ID != "browser" {
		t.Errorf("buttons %+v", b)
	}
}

// Reply… sends the text to the thread's first comment and reopens the
// same thread; Resolve resolves it, and a resolved thread offers Reopen.
func TestPRThreadReplyAndResolve(t *testing.T) {
	m, f := prThreadsModel(t)
	m.handlePRPickerSelect("thread-PRRT_1")
	if cmd, _ := m.handlePRConfirm("reply"); cmd != nil || !m.promptOpen || m.promptKind != promptKindPRReply {
		t.Fatal("reply prompt not open")
	}
	cmd, ok := m.handlePRPromptSubmit("ok, a slice")
	if !ok || cmd == nil {
		t.Fatal("no reply command")
	}
	done := cmd().(prDoneMsg)
	if done.err != nil || !done.reopen || !f.called("api --method POST repos/{owner}/{repo}/pulls/14/comments/501/replies -f body=ok, a slice") {
		t.Fatalf("done %+v calls %v", done, f.calls)
	}
	m.confirmOpen = false
	m.onPRDone(done)
	if !toastHas(m, "Replied on parser.go:12") {
		t.Error("no reply toast")
	}
	prDo(t, m, m.prViewCmd(14)) // the reopen
	if !m.confirmOpen || m.confirmKind != confirmKindPRThread || m.prS().threadOn().ID != "PRRT_1" {
		t.Fatal("the thread did not come back after the reply")
	}

	cmd, _ = m.handlePRConfirm("resolve")
	done = cmd().(prDoneMsg)
	if done.err != nil || !f.called("api graphql -f query=mutation($id:ID!){resolveReviewThread") {
		t.Fatalf("resolve: %+v %v", done, f.calls)
	}
	m.onPRDone(done)
	if !toastHas(m, "Resolved parser.go:12") {
		t.Error("no resolve toast")
	}

	m.handlePRPickerSelect("thread-PRRT_2")
	b := m.confirm.Buttons()
	if b[2].ID != "resolve" || b[2].Title != "Reopen Thread" {
		t.Errorf("resolved thread buttons %+v", b)
	}
	cmd, _ = m.handlePRConfirm("resolve")
	cmd()
	if !f.called("api graphql -f query=mutation($id:ID!){unresolveReviewThread") {
		t.Errorf("reopen not sent: %v", f.calls)
	}
}
