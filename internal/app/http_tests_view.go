package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The Response page's Tests view (API-8): one row per `# @assert`,
// `# @pre` and `# @post` line of the request, passed in green, failed
// in red with the reason. Row 2 and the lens add "2/3 checks".

// httpRunTests runs the checks of one finished send; values the
// scripts read never reach the page (only pass / fail and reasons).
func httpRunTests(rr httpclient.Resolved, resp *httpclient.Response) []httpclient.AssertResult {
	if !rr.HasTests() {
		return nil
	}
	res := httpclient.RunTests(rr, resp, httpSession)
	for i := range res {
		res[i].Message = ai.Redact(httpHideSecrets(res[i].Message, rr.Secrets))
	}
	return res
}

func httpHideSecrets(s string, secrets []string) string {
	for _, v := range secrets {
		if len(v) >= 4 {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}

// httpTestsShort is the lens text: "✓ 3/3 checks" or "✗ 1/3 checks".
func httpTestsShort(res []httpclient.AssertResult) string {
	if len(res) == 0 {
		return ""
	}
	n := httpTestsPassed(res)
	mark := httpGlyphCheckOK.String()
	if n < len(res) {
		mark = httpGlyphCheckFail.String()
	}
	return mark + " " + itoa(n) + "/" + itoa(len(res)) + " checks"
}

func httpTestsPassed(res []httpclient.AssertResult) int {
	n := 0
	for _, r := range res {
		if r.Pass {
			n++
		}
	}
	return n
}

// httpTestRows are the Tests view's rows.
func httpTestRows(run *httpRun) []httpRow {
	if len(run.tests) == 0 {
		return []httpRow{
			{k: "note", text: "This request has no checks. Add lines above the request line:"},
			{text: "  # @assert status == 200"},
			{text: "  # @assert body $.id exists"},
			{text: `  # @assert body schema {"type":"object","required":["id"]}`},
			{text: "  # @pre set id = {{$uuid}}"},
			{text: "  # @post set token = body $.token"},
		}
	}
	n := httpTestsPassed(run.tests)
	title := "TESTS  " + httpclient.AssertSummary(run.tests)
	rows := []httpRow{{k: "t", text: title, n: len("TESTS")}}
	for _, r := range run.tests {
		if r.Pass {
			rows = append(rows, httpRow{k: "add", text: "  " + httpGlyphCheckOK.String() + " " + r.Expr})
			continue
		}
		rows = append(rows, httpRow{k: "del", text: "  " + httpGlyphCheckFail.String() + " " + r.Expr})
		if r.Message != "" {
			rows = append(rows, httpRow{k: "note", text: "      " + r.Message})
		}
	}
	if n == len(run.tests) {
		rows = append(rows, httpRow{}, httpRow{k: "note", text: "All checks passed."})
	}
	return rows
}

// httpAITestsText is the checks block on top of an http_send result
// ("" with no checks): pass / fail and reasons, never script values.
func httpAITestsText(res []httpclient.AssertResult) string {
	if len(res) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("checks: " + httpclient.AssertSummary(res) + "\n")
	for _, r := range res {
		mark := "pass"
		if !r.Pass {
			mark = "FAIL"
		}
		b.WriteString("  " + mark + " " + r.Expr)
		if r.Message != "" {
			b.WriteString(" — " + r.Message)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// httpScriptNames are the variables the file's @pre / @post lines set.
func httpScriptNames(f httpclient.File) map[string]bool {
	var out map[string]bool
	for _, r := range f.Requests {
		for i, l := range append(append([]string(nil), r.Pre...), r.Post...) {
			if st, err := httpclient.ParseScript(l, i < len(r.Pre)); err == nil && st.Op == "set" {
				if out == nil {
					out = map[string]bool{}
				}
				out[st.Name] = true
			}
		}
	}
	return out
}

// httpClearScriptVars is HTTP: Clear Script Variables.
func (m *Model) httpClearScriptVars() tea.Cmd {
	n := len(httpSession.Names())
	httpSession.Clear()
	if n == 0 {
		return m.pushToastDetail(toast.Info, "No script variables", "`# @pre set` and `# @post set` lines fill them.")
	}
	return m.pushToastDetail(toast.Info, "Script variables cleared", itoa(n)+" removed")
}
