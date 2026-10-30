//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// TESTING and TEST RESULTS (WEBP-4) against the fake hub.

// openRunView logs in and opens a Run, Test and Debug view: on the
// desktop its activity bar item, on the phone its More link.
func openRunView(t *testing.T, vp Viewport, act, sub, topic string) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	c := p.Login("c-run")
	if vp.W >= 1200 {
		p.Tap(`nav .act-item[data-act="` + act + `"]`)
	} else {
		p.GoTab("more")
		p.Tap(`.more-link[data-sub="` + sub + `"]`)
	}
	c.WaitSub(t, topic)
	return p, c
}

func testsTree(state string, failed int) webproto.TestsSnap {
	b := webproto.TestItem{ID: "c:pkg/a_test.go::TestB", Kind: "case", Parent: "f:pkg/a_test.go", Label: "TestB", Path: "pkg/a_test.go", Line: 9, State: "pending", Debug: true}
	if failed > 0 {
		b.State, b.Message, b.FailPath, b.FailLine = "failed", "a_test.go:11: want 2, got 3", "pkg/a_test.go", 11
	}
	return webproto.TestsSnap{Framework: "go test", State: state, Command: "go test -json ./...", ElapsedMs: 1200,
		Counts: webproto.TestCounts{Total: 2, Passed: 1, Failed: failed},
		Items: []webproto.TestItem{
			{ID: "p:pkg", Kind: "package", Label: "pkg", State: "failed"},
			{ID: "f:pkg/a_test.go", Kind: "file", Parent: "p:pkg", Label: "a_test.go", Path: "pkg/a_test.go", State: "failed"},
			{ID: "c:pkg/a_test.go::TestA", Kind: "case", Parent: "f:pkg/a_test.go", Label: "TestA", Path: "pkg/a_test.go", Line: 3, State: "passed", Ms: 12, Debug: true},
			b,
		}}
}

func txRow(id string) string { return `document.querySelector('.tx-row[data-id="` + id + `"]')` }

// The tree shows each row's state; a test's own Run and Debug buttons
// and the head's Run All, Rerun Failed and Stop send the laptop the row's
// id (never a command); the filter keeps matching tests with their
// parents; the summary opens TEST RESULTS with the run's output, as text.
func TestTestingTreeRunsAndShowsResults(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openRunView(t, vp, "testing", "testing", "tests")
			c.Send(t, Snap("tests", 1, testsTree("idle", 1)))
			p.WaitJS(`document.querySelectorAll(".tx-row").length === 4`)
			if !Eval[bool](p, `!!`+txRow("c:pkg/a_test.go::TestB")+`.querySelector(".tx-failed") && !!`+txRow("c:pkg/a_test.go::TestA")+`.querySelector(".tx-passed")`) {
				t.Fatal("the states do not show")
			}
			if got := Eval[string](p, txRow("c:pkg/a_test.go::TestA")+`.querySelector(".tx-desc").textContent`); got != "12 ms" {
				t.Errorf("duration %q", got)
			}
			Eval[bool](p, `(`+txRow("c:pkg/a_test.go::TestA")+`.focus(), true)`)
			p.TapJS(txRow("c:pkg/a_test.go::TestA") + `.querySelector('[data-act="run"]')`)
			reply(t, c, expectOp(t, c, webproto.OpTestsRun, `{"id":"c:pkg/a_test.go::TestA"}`), nil)
			Eval[bool](p, `(`+txRow("c:pkg/a_test.go::TestB")+`.focus(), true)`)
			p.TapJS(txRow("c:pkg/a_test.go::TestB") + `.querySelector('[data-act="debug"]')`)
			reply(t, c, expectOp(t, c, webproto.OpTestsDebug, `{"id":"c:pkg/a_test.go::TestB"}`), nil)
			p.Tap(`.rt [data-act="run-failed"]`)
			reply(t, c, expectOp(t, c, webproto.OpTestsRun, `{"failed":true}`), nil)
			p.Tap(`.rt [data-act="run-all"]`)
			reply(t, c, expectOp(t, c, webproto.OpTestsRun, `{}`), nil)

			running := testsTree("running", 0)
			running.Live = true
			c.Send(t, Delta("tests", 2, running))
			p.WaitJS(`document.querySelector('.rt [data-act="stop"]') && document.querySelector(".tx-bar")`)
			p.Tap(`.rt [data-act="stop"]`)
			reply(t, c, expectOp(t, c, webproto.OpTestsStop, `{}`), nil)
			c.Send(t, Delta("tests", 3, testsTree("idle", 1)))
			p.WaitJS(`!document.querySelector(".tx-bar") && document.querySelector(".tx-sum").textContent.includes("1/2 tests passed")`)

			p.Do(chromedp.SendKeys(".tx-filter", "testb"))
			p.WaitJS(`[...document.querySelectorAll(".tx-row .tx-label")].map((e) => e.textContent).join() === "pkg,a_test.go,TestB"`)

			if vp.W >= 1200 {
				p.Tap(`.tx-sum .tx-link`)
				r := expectOp(t, c, webproto.OpTestsOutput, `{}`)
				reply(t, c, r, webproto.TestOutput{Lines: []string{"=== RUN   TestB", "<b>--- FAIL: TestB</b>"}})
				p.WaitJS(`document.querySelectorAll('.wb-panel .tres-rows .trow').length === 2 && document.querySelector('.wb-panel-title.is-on')?.dataset.panel === "test-results"`)
				if Eval[int](p, `document.querySelectorAll(".tres-rows b").length`) != 0 {
					t.Fatal("output became HTML")
				}
			}
			if len(p.Problems()) > 0 {
				t.Errorf("problems: %v", p.Problems())
			}
		})
	}
}

// A viewer sees the tree but no button that runs anything.
func TestTestingViewerOnlyLooks(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	meta := Meta("c-view")
	meta.Role = webproto.RoleViewer
	c := p.LoginMeta(meta)
	p.Tap(`nav .act-item[data-act="testing"]`)
	c.WaitSub(t, "tests")
	c.Send(t, Snap("tests", 1, testsTree("idle", 1)))
	p.WaitJS(`document.querySelectorAll(".tx-row").length === 4`)
	if n := Eval[int](p, `document.querySelectorAll('.tx [data-act="run"], .tx [data-act="debug"], .rt').length`); n != 0 {
		t.Fatalf("%d run buttons for a viewer", n)
	}
	if !strings.Contains(p.Text(".tx-sum"), "1/2 tests passed") {
		t.Errorf("summary %q", p.Text(".tx-sum"))
	}
}
