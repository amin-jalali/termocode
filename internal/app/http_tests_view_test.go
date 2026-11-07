package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// API-8: the Tests view lists each check; row 2 and the lens count them.
func TestHTTPTestsViewAndSummary(t *testing.T) {
	unicodeIcons(t)
	run := jsonRun(`{"id":1,"token":"abc"}`)
	run.rr.Asserts = []string{"status == 200", "body $.id == 2"}
	run.rr.Post = []string{"set tok = body $.token"}
	run.tests = httpRunTests(run.rr, run.resp)
	if len(run.tests) != 3 {
		t.Fatalf("tests = %+v", run.tests)
	}
	p := buildHTTPPage(run, httpViewTests, false, false)
	text := strings.Join(p.lines, "\n")
	for _, want := range []string{"TESTS  2 of 3 checks passed", "✓ status == 200", "✗ body $.id == 2", "body $.id was 1", "✓ @post set tok = body $.token"} {
		if !strings.Contains(text, want) {
			t.Errorf("tests view lacks %q:\n%s", want, text)
		}
	}
	if httpRowKind(p, "  ✗ body $.id == 2") != "del" || httpRowKind(p, "  ✓ status == 200") != "add" {
		t.Fatalf("row kinds wrong:\n%s", text)
	}
	if p.lines[1] == "" || !strings.Contains(p.lines[1], "✗ 2/3 checks") {
		t.Fatalf("row 2 = %q", p.lines[1])
	}
	if v, _ := httpSession.Get("tok"); v != "abc" {
		t.Fatalf("session tok = %q", v)
	}
	httpSession.Clear()
	empty := jsonRun(`{}`)
	if !strings.Contains(strings.Join(buildHTTPPage(empty, httpViewTests, false, false).lines, "\n"), "# @assert status == 200") {
		t.Fatal("no-checks view does not show how to add one")
	}
	if httpAITestsText(run.tests) == "" || !strings.Contains(httpAITestsText(run.tests), "FAIL body $.id == 2") {
		t.Fatalf("AI text = %q", httpAITestsText(run.tests))
	}
}

// API-8: names set by @pre/@post lines count as known in the editor.
func TestHTTPScriptNamesAreKnown(t *testing.T) {
	f := httpclient.Parse("# @pre set a = 1\n# @post set b = status\n# @post unset c\nGET https://x/{{a}}\n")
	n := httpScriptNames(f)
	if !n["a"] || !n["b"] || n["c"] {
		t.Fatalf("names = %v", n)
	}
}
