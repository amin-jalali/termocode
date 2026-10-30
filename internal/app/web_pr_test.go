package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestPR is a web hub on a git workspace whose gh is the fake.
func webTestPR(t *testing.T, f *prFakeGH) (*Model, *webConn) {
	t.Helper()
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestRepo(t, &m)
	m.ciW().run = f.run
	return &m, webTestSub(t, &m)
}

// Spec (pull requests): pr.list answers the open pull requests through
// the laptop's gh with each one's checks summed up; pr.view one pull
// request with its body, files, reviews and checks. No gh, or no GitHub
// remote, is said plainly.
func TestPRListAndView(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": prListText, "pr view": prViewText14}}
	m, c := webTestPR(t, f)

	var l webproto.PRList
	webTestData(t, webTestCallBg(t, m, c, webproto.OpPRList, `{}`), &l)
	if len(l.Items) != 2 {
		t.Fatalf("list %+v", l)
	}
	if p := l.Items[0]; p.Number != 14 || p.Author != "ann" || p.Branch != "feat/parser" || p.Checks != "pass" || p.Review != "APPROVED" || p.URL == "" {
		t.Errorf("first %+v", p)
	}
	if p := l.Items[1]; !p.Draft || p.Checks != "fail" {
		t.Errorf("second %+v", p)
	}

	var v webproto.PRView
	webTestData(t, webTestCallBg(t, m, c, webproto.OpPRView, `{"number":14}`), &v)
	if v.Item.Number != 14 || v.State != "OPEN" || v.Body == "" || len(v.Files) == 0 || v.Files[0].Path == "" || len(v.Checks) == 0 {
		t.Errorf("view %+v", v)
	}

	f.errs = map[string]error{"pr list": ghci.ErrNoGH}
	if got := webTestErr(webTestCallBg(t, m, c, webproto.OpPRList, `{}`)); got != string(webproto.CodeConflict) {
		t.Errorf("no gh: %q", got)
	}
	f.errs = map[string]error{"pr list": errors.New("exit status 1")}
	if got := webTestErr(webTestCallBg(t, m, c, webproto.OpPRList, `{}`)); got == "" {
		t.Error("a gh failure answered no error")
	}
}

// Spec (CI): ci.status answers the laptop's own CI watch of the branch:
// its runs, their state and the branch's pull request; before the first
// answer it says it is loading.
func TestCIStatus(t *testing.T) {
	m, c := webTestPR(t, &prFakeGH{})
	w := m.ciW()
	w.branch, w.loaded = "main", true
	w.snap = ghci.Snapshot{Branch: "main", HeadSHA: "60ed1c82aa", Runs: []ghci.Run{
		{ID: 1, Workflow: "CI", Title: "fix it", Status: "completed", Conclusion: "failure", URL: "https://x/1"},
		{ID: 2, Workflow: "Lint", Title: "fix it", Status: "in_progress"}},
		PR: &ghci.PR{Number: 7, Title: "Fix", State: "OPEN", URL: "https://x/pr/7", Checks: []ghci.Check{{Name: "test", State: ghci.StateFail}}}}
	var s webproto.CIStatus
	webTestData(t, webTestCallBg(t, m, c, webproto.OpCIStatus, `{}`), &s)
	if s.Branch != "main" || s.Loading || s.State != "fail" || s.Head != "60ed1c8" || len(s.Runs) != 2 || s.Runs[0].State != "fail" || s.Runs[1].State != "pending" {
		t.Errorf("status %+v", s)
	}
	if s.PR == nil || s.PR.Number != 7 || s.PR.Checks != "fail" {
		t.Errorf("pr %+v", s.PR)
	}
	w.loaded = false
	s = webproto.CIStatus{}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpCIStatus, `{}`), &s)
	if !s.Loading {
		t.Errorf("before the first answer: %+v", s)
	}
}

// Spec (pull requests): merge, review and check out wait for a laptop
// card that names the pull request (and warns as the laptop's own merge
// card does); gh runs only after the laptop's yes, never with an admin
// override. They need the pull request read first (pr.view).
func TestPRActionsNeedLaptopCard(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": prListText, "pr view": prViewText14, "pr merge": "", "pr review": "", "pr checkout": ""}}
	m, c := webTestPR(t, f)
	if got := webTestErr(webTestCallBg(t, m, c, webproto.OpPRMerge, `{"number":14,"method":"squash"}`)); got != string(webproto.CodeNotFound) {
		t.Errorf("not read yet: %q", got)
	}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpPRView, `{"number":14}`), &webproto.PRView{})

	for _, tc := range []struct{ args, op, want, call string }{
		{`{"number":14,"method":"squash"}`, webproto.OpPRMerge, "conflicts with main", "pr merge 14 --squash"},
		{`{"number":14,"kind":"approve"}`, webproto.OpPRReview, "Approve #14", "pr review 14 --approve"},
		{`{"number":14,"kind":"comment","body":"Nice work"}`, webproto.OpPRReview, "Nice work", "pr review 14 --comment"},
		{`{"number":14}`, webproto.OpPRCheckout, "feat/parser", "pr checkout 14"},
	} {
		var ref webproto.AskRef
		webTestData(t, webTestCallBg(t, m, c, tc.op, tc.args), &ref)
		if ref.Card == "" || !m.confirmOpen || m.confirm.SafeID() != "cancel" {
			t.Fatalf("%s: no laptop card", tc.op)
		}
		if v := m.confirm.View(); !strings.Contains(v, tc.want) {
			t.Errorf("%s card has no %q:\n%s", tc.op, tc.want, v)
		}
		if f.called(tc.call) {
			t.Fatalf("%s ran before the laptop said so", tc.call)
		}
		m.confirmOpen = false
		webDrain(t, m, m.handleConfirmSelect("confirm"))
		if !f.called(tc.call) {
			t.Errorf("%s did not run after the laptop's yes: %v", tc.call, f.calls)
		}
	}
	for _, c := range f.calls {
		if strings.Contains(c, "--admin") {
			t.Errorf("an admin override: %s", c)
		}
	}
}
