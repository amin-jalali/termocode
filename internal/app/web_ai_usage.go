package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// ai.usage (MOB-73): this session's AI usage for the phone's usage view,
// from the same in-memory log the laptop's USAGE view and the
// usage_summary tool read. Numbers and names only.

func init() {
	webOps[webproto.OpAIUsage] = (*Model).webAIUsage
}

func (m *Model) webAIUsage(call *webCall) tea.Cmd {
	call.reply(m.webAIUsageData())
	return nil
}

func (m *Model) webAIUsageData() webproto.AIUsage {
	var log []aiUsageEntry
	if m.ai != nil {
		log = m.ai.usageLog
	}
	sum := aiUsageSummarize(log, aiUsageByModel)
	total := webproto.AIUsageLine{Name: "total", Calls: sum.Total.Calls, Input: sum.Total.In, Output: sum.Total.Out}
	if sum.Total.Priced > 0 {
		total.CostUSD, total.Priced = sum.Total.Cost, true
	}
	return webproto.AIUsage{Scope: "this session", ByModel: webUsageLines(aiUsageLines(log, aiUsageByModel)),
		ByAgent: webUsageLines(aiUsageLines(log, aiUsageByAgent)), Total: total}
}

// webUsageLines turns usage_summary's lines into the wire's, capped.
func webUsageLines(in []aiUsageLine) []webproto.AIUsageLine {
	out := make([]webproto.AIUsageLine, 0, min(len(in), webproto.MaxUsageLines))
	for _, l := range in {
		if len(out) == webproto.MaxUsageLines {
			break
		}
		w := webproto.AIUsageLine{Name: aiCardText(l.Name, webproto.MaxUsageName), Calls: l.Calls, Input: l.TokensIn, Output: l.TokensOut, Billing: l.Billing}
		if l.CostUSD != nil {
			w.CostUSD, w.Priced = *l.CostUSD, true
		}
		out = append(out, w)
	}
	return out
}
