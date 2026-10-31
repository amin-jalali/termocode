package agentrun

import (
	"strings"
	"testing"
)

// Spec (OM-3): the verdict is the last VERDICT line outside code blocks.
func TestParseVerdict(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		pass, ok   bool
		report     string
	}{
		{"pass", "All tests pass.\nVERDICT: PASS", true, true, "All tests pass."},
		{"fail with report", "Ran go test.\nVERDICT: FAIL\na.go:3 nil map\nb.go:9 typo", false, true, "Ran go test.\na.go:3 nil map\nb.go:9 typo"},
		{"lower case", "ok\nverdict: pass", true, true, "ok"},
		{"bold", "**VERDICT: FAIL**\nx.go:1 broken", false, true, "x.go:1 broken"},
		{"missing", "Looks fine to me.", false, false, "Looks fine to me."},
		{"in code block", "```\nVERDICT: PASS\n```\nI could not run the tests.", false, false, "```\nVERDICT: PASS\n```\nI could not run the tests."},
		{"last wins", "VERDICT: FAIL\nfixed now\nVERDICT: PASS", true, true, "VERDICT: FAIL\nfixed now"},
		{"not a verdict", "VERDICTS: PASSING", false, false, "VERDICTS: PASSING"},
	} {
		pass, report, ok := ParseVerdict(tc.text)
		if pass != tc.pass || ok != tc.ok || report != tc.report {
			t.Errorf("%s: got pass=%v ok=%v report=%q, want %v %v %q", tc.name, pass, ok, report, tc.pass, tc.ok, tc.report)
		}
	}
	if !strings.Contains(VerifyPrompt, "VERDICT: PASS") || !strings.Contains(VerifyRepairPrompt, "VERDICT: FAIL") {
		t.Error("the prompts must name the verdict line")
	}
}

// OMX-14: the verify step lists its evidence under EVIDENCE:, one item
// a line (a "- " is dropped), up to the next blank line.
func TestParseEvidence(t *testing.T) {
	text := "Checked it.\nEVIDENCE:\n- go test ./... → ok\n* read parser.go:12\n\nVERDICT: PASS"
	got := ParseEvidence(text)
	if len(got) != 2 || got[0] != "go test ./... → ok" || got[1] != "read parser.go:12" {
		t.Fatalf("evidence %q", got)
	}
	if ParseEvidence("VERDICT: PASS") != nil {
		t.Fatal("evidence without a list")
	}
	if !strings.Contains(VerifyPrompt, "EVIDENCE:") {
		t.Fatal("the verify prompt does not ask for evidence")
	}
}
