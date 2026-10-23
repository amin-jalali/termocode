package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

func usd(v float64) *float64 { return &v }

// usageEntries: two runs, two models, local model, one unknown price.
func usageEntries() []aiUsageEntry {
	return []aiUsageEntry{
		{Run: "r1", Agent: "Reviewer", Model: "claude-sonnet-4-5", U: ai.Usage{Input: 1800, Output: 60, Requests: 1}, Cost: usd(0.0063)},
		{Run: "r1", Agent: "Reviewer", Model: "claude-sonnet-4-5", U: ai.Usage{Input: 2100, Output: 140, CacheRead: 500, Requests: 2}, Cost: usd(0.0094)},
		{Run: "r1", Agent: "Fixer", Model: "gpt-5-mini", U: ai.Usage{Input: 9000, Output: 800, Requests: 1}, Cost: usd(0.0039)},
		{Run: "r2", Agent: "Fixer", Model: "qwen2.5-coder:7b", U: ai.Usage{Input: 3000, Output: 200, Requests: 1}, Cost: usd(0)},
		{Run: "r2", Agent: "Helper", Model: "mystery-1", U: ai.Usage{Input: 100, Output: 10, Requests: 1}},
	}
}

func rowByName(t *testing.T, rows []aiUsageRow, name string) aiUsageRow {
	t.Helper()
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", name, rows)
	return aiUsageRow{}
}

// Spec (AU-4): usage adds up by model and by agent; totals add up.
func TestAIUsageSummarize(t *testing.T) {
	agentsUnicode(t)
	sum := aiUsageSummarize(usageEntries(), aiUsageByModel)
	son := rowByName(t, sum.Rows, "claude-sonnet-4-5")
	// 3 requests; prompt = 1800 + 2100 + 500 cached = 4400; out 200.
	if son.Calls != 3 || son.In != 4400 || son.Out != 200 || son.Total != 4600 {
		t.Errorf("sonnet row %+v", son)
	}
	if got := son.costText(); got != "$0.016" {
		t.Errorf("sonnet cost %q", got)
	}
	if got := rowByName(t, sum.Rows, "qwen2.5-coder:7b").costText(); got != "$0" {
		t.Errorf("local model cost %q, want $0", got)
	}
	if got := rowByName(t, sum.Rows, "mystery-1").costText(); got != "—" {
		t.Errorf("unknown price cost %q", got)
	}
	var calls, in, out, total int
	for _, r := range sum.Rows {
		calls, in, out, total = calls+r.Calls, in+r.In, out+r.Out, total+r.Total
	}
	if sum.Total.Calls != calls || sum.Total.In != in || sum.Total.Out != out || sum.Total.Total != total || calls != 6 {
		t.Errorf("total %+v vs rows sum %d/%d/%d/%d", sum.Total, calls, in, out, total)
	}
	if sum.Total.In+sum.Total.Out != sum.Total.Total {
		t.Errorf("in + out != total: %+v", sum.Total)
	}
	// Some prices are known, one is not: the total says so.
	if got := sum.Total.costText(); got != "$0.020+" {
		t.Errorf("total cost %q", got)
	}
	if len(sum.Unknown) != 1 || sum.Unknown[0] != "mystery-1" {
		t.Errorf("unknown %v", sum.Unknown)
	}
}

// Sorted by cost, then tokens; an unknown price counts as no cost.
func TestAIUsageSorted(t *testing.T) {
	sum := aiUsageSummarize(usageEntries(), aiUsageByModel)
	var names []string
	for _, r := range sum.Rows {
		names = append(names, r.Name)
	}
	want := "claude-sonnet-4-5,gpt-5-mini,qwen2.5-coder:7b,mystery-1"
	if got := strings.Join(names, ","); got != want {
		t.Errorf("order %s, want %s", got, want)
	}
}

func TestAIUsageByAgent(t *testing.T) {
	agentsUnicode(t)
	sum := aiUsageSummarize(usageEntries(), aiUsageByAgent)
	fixer := rowByName(t, sum.Rows, "Fixer")
	if fixer.Calls != 2 || fixer.In != 12000 || fixer.Out != 1000 || fixer.costText() != "$0.004" {
		t.Errorf("Fixer %+v cost %s", fixer, fixer.costText())
	}
	if got := rowByName(t, sum.Rows, "Helper").costText(); got != "—" {
		t.Errorf("Helper cost %q", got)
	}
	if len(sum.Rows) != 3 {
		t.Errorf("rows %+v", sum.Rows)
	}
}

// A Claude sign-in is a subscription: no price, and no "set ai_prices".
func TestAIUsageSubscription(t *testing.T) {
	agentsUnicode(t)
	sum := aiUsageSummarize([]aiUsageEntry{
		{Agent: "Assistant", Model: "claude-sonnet-4-5", U: ai.Usage{Input: 10, Output: 5, Requests: 1}, Sub: true},
	}, aiUsageByModel)
	if len(sum.Unknown) != 0 || len(sum.Sub) != 1 {
		t.Errorf("unknown %v sub %v", sum.Unknown, sum.Sub)
	}
	if got := sum.Rows[0].costText(); got != "—" {
		t.Errorf("cost %q", got)
	}
}

// aiUsageModel is a chat that ran the review profile twice, with a
// local plain turn in between (session) in the log.
func usageModel(t *testing.T) *Model {
	t.Helper()
	r := ai.Chunk{Delta: "Problem in a.txt.", Usage: &ai.Usage{Model: "claude-sonnet-4-5", Input: 4000, Output: 100, Requests: 1, PeakContext: 4000}}
	f := ai.Chunk{Delta: "Fixed.", Usage: &ai.Usage{Model: "gpt-5-mini", Input: 9000, Output: 800, Requests: 1, PeakContext: 9000}}
	rev := &recordingAIProvider{replies: []ai.Chunk{r, r}}
	fix := &recordingAIProvider{replies: []ai.Chunk{f, f}}
	m, _ := aiRunModel(t, rev, fix)
	m.output = newOutputStore()
	driveAIChat(t, m, m.sendAIChat("first"), nil)
	driveAIChat(t, m, m.sendAIChat("second"), nil)
	m.actionsOpen, m.actionsPinned = true, true
	return m
}

// Spec (AU-4): "this run" is the run you select with the arrow keys;
// "this session" has every run of this UI process.
func TestAIUsageRunAndSession(t *testing.T) {
	agentsUnicode(t)
	m := usageModel(t)
	if n := len(m.ai.usageLog); n != 4 {
		t.Fatalf("session log has %d entries, want 4 (2 runs x 2 steps)", n)
	}
	m.dispatchAIPalette("ai-usage")
	if !m.ai.agents.on || !m.ai.agents.usage {
		t.Fatal("AI: Usage did not open the usage screen")
	}
	text := agentsText(m, 80, 40)
	wantAll(t, text, "USAGE", "THIS RUN", "by model", "by agent", "THIS SESSION",
		"claude-sonnet-4-5", "gpt-5-mini", "Reviewer", "Fixer", "total", "$0.017", "$0.035", "(2/2)")
	// Left shows the first run; the session table does not change.
	m.agentsKey(t, "left")
	run1 := agentsText(m, 80, 40)
	wantAll(t, run1, "(1/2)", "$0.017", "$0.035")
	m.agentsKey(t, "u") // back to AGENTS
	if m.ai.agents.usage {
		t.Error("u did not leave the usage screen")
	}
}

// Without a run in the chat the run tables say so; the screen is only
// a view: nothing is written to disk.
func TestAIUsageEmptyAndNoDiskWrites(t *testing.T) {
	agentsUnicode(t)
	m, dir := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	m.actionsOpen, m.actionsPinned = true, true
	before := dirListing(t, dir)
	m.dispatchAIPalette("ai-usage")
	text := agentsText(m, 80, 30)
	wantAll(t, text, "No pipeline run in this chat yet", "No model calls in this session yet")
	m.ai.recordUsage(aiUsageEntry{Agent: "Assistant", Model: "gpt-5-mini", U: ai.Usage{Input: 5, Output: 5, Requests: 1}, Cost: usd(0)})
	text = agentsText(m, 80, 30)
	wantAll(t, text, "gpt-5-mini")
	if after := dirListing(t, dir); after != before {
		t.Errorf("the usage screen changed the workspace:\n%s\n--- after\n%s", before, after)
	}
	for _, f := range aiUsageFiles(t) {
		t.Errorf("usage data on disk: %s", f)
	}
}

func dirListing(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil {
			b.WriteString(p + " " + info.ModTime().String() + "\n")
		}
		return nil
	})
	return b.String()
}

// aiUsageFiles are files under the config dirs whose name has "usage".
func aiUsageFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, root := range []string{os.Getenv("HOME"), os.Getenv("XDG_CONFIG_HOME"), os.Getenv("XDG_DATA_HOME")} {
		if root == "" {
			continue
		}
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.Contains(strings.ToLower(info.Name()), "usage") {
				out = append(out, p)
			}
			return nil
		})
	}
	return out
}

// Narrow panels drop the in / out columns, wide ones show them; no row
// is wider than the panel.
func TestAIUsageFitsWidths(t *testing.T) {
	agentsUnicode(t)
	m := usageModel(t)
	m.dispatchAIPalette("ai-usage")
	for _, w := range []int{44, 60, 80} {
		text := agentsText(m, w, 40)
		for _, l := range strings.Split(text, "\n") {
			if got := len([]rune(l)); got > w {
				t.Errorf("width %d: row of %d cells: %q", w, got, l)
			}
		}
		if w >= 80 && !strings.Contains(text, "out") {
			t.Errorf("width %d lacks the in/out columns:\n%s", w, text)
		}
		if w == 44 && (strings.Contains(text, " out ") || !strings.Contains(text, "total")) {
			t.Errorf("width 44 should keep calls, total and cost only:\n%s", text)
		}
	}
}

// The unknown-price note names the model.
func TestAIUsageUnknownPriceNote(t *testing.T) {
	agentsUnicode(t)
	m := usageModel(t)
	m.ai.recordUsage(aiUsageEntry{Run: "x", Agent: "Helper", Model: "mystery-1", U: ai.Usage{Input: 100, Output: 10, Requests: 1}})
	m.dispatchAIPalette("ai-usage")
	text := agentsText(m, 80, 60)
	wantAll(t, text, "price unknown for mystery-1 (set ai_prices)", "—")
}

// A plain chat turn (default profile) lands in the session table too.
func TestAIUsagePlainTurnRecorded(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{{Delta: "hi",
		Usage: &ai.Usage{Model: "claude-sonnet-4-5", Input: 1000, Output: 100, Requests: 1, PeakContext: 1000}}}}
	m, _ := aiIDETestModel(t, p)
	m.ai.cfg = ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}
	m.output = newOutputStore()
	driveAIChat(t, m, m.sendAIChat("hello"), nil)
	if len(m.ai.usageLog) != 1 {
		t.Fatalf("log %+v", m.ai.usageLog)
	}
	e := m.ai.usageLog[0]
	if e.Model != "claude-sonnet-4-5" || e.U.Input != 1000 || e.Cost == nil || e.Agent == "" {
		t.Errorf("entry %+v", e)
	}
}

// The run summary row opens the usage screen when its tokens and cost
// are clicked; the palette entry exists and is never for the AI.
func TestAIUsageRunRowLinkAndPolicy(t *testing.T) {
	agentsUnicode(t)
	m := usageModel(t)
	lines := m.buildAITranscript(100)
	found := ""
	for _, l := range lines {
		for _, b := range l.buttons {
			if strings.HasPrefix(b.id, "usage:") {
				found = b.id
			}
		}
	}
	if found == "" {
		t.Fatal("the run row has no usage link")
	}
	m.onAIButton(found)
	if !m.ai.agents.on || !m.ai.agents.usage {
		t.Error("clicking the link did not open the usage screen")
	}
	ok := false
	for _, e := range aiPaletteItems() {
		ok = ok || (e.id == "ai-usage" && e.title == "AI: Usage")
	}
	if !ok {
		t.Error("no AI: Usage palette entry")
	}
	if pol, has := aiActionPolicyFor("ai-usage"); !has || pol.Gate != aiGateNever || pol.Reason == "" {
		t.Errorf("policy %+v", pol)
	}
	_ = tea.KeyMsg{}
}
