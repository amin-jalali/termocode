package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// aiAuditUsage writes the usage line of a finished turn to the AI audit
// channel (U20 AP-1):
//
//	09:05:00  usage  claude-sonnet-5-5 · 2 requests · 4 103 in · 812 out · 1 000 cached · $0.017 · context 1%
//
// A provider that reports no counts writes no line.
func (m *Model) aiAuditUsage(ev ai.Event) {
	if ev.Usage == nil {
		return
	}
	s := m.aiS()
	text := aiUsageText(s.cfg.Kind, *ev.Usage, ai.NewPricing(s.settings.Prices), ai.NewContextWindows(s.settings.ContextWindows))
	m.aiRecordTurnUsage(*ev.Usage)
	m.output.Append(aiAuditChannel, clock.Now().Format("15:04:05")+"  usage  "+text)
}

// aiUsageText is model · tokens · cost · context use. The cost is left
// out for a model with no known price, the context use for one with no
// known window.
func aiUsageText(kind string, u ai.Usage, prices ai.Pricing, windows ai.ContextWindows) string {
	model := u.Model
	if model == "" {
		model = "?"
	}
	parts := []string{model}
	if u.Requests > 1 {
		parts = append(parts, fmt.Sprintf("%d requests", u.Requests))
	}
	parts = append(parts, aiGroupDigits(u.Input)+" in", aiGroupDigits(u.Output)+" out")
	if u.CacheRead > 0 {
		parts = append(parts, aiGroupDigits(u.CacheRead)+" cached")
	}
	if u.CacheWrite > 0 {
		parts = append(parts, aiGroupDigits(u.CacheWrite)+" cache write")
	}
	if usd, ok := prices.Cost(kind, u); ok {
		parts = append(parts, aiDollars(usd))
	}
	if w, ok := windows.Lookup(u.Model); ok && u.PeakContext > 0 {
		pct, _ := ai.ContextPercent(u, w)
		parts = append(parts, fmt.Sprintf("context %.0f%%", pct))
	}
	return strings.Join(parts, " · ")
}

// aiGroupDigits writes n with a space every three digits (4 103).
func aiGroupDigits(n int) string {
	if n < 0 {
		return "-" + aiGroupDigits(-n)
	}
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// aiDollars formats a cost: $0, <$0.001, $0.017, $5.00.
func aiDollars(usd float64) string {
	switch {
	case usd == 0:
		return "$0"
	case usd < 0.001:
		return "<$0.001"
	case usd < 1:
		return fmt.Sprintf("$%.3f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// aiRecordTurnUsage adds a plain chat turn (a one-step profile, no run)
// to the session log that AI: Usage shows.
func (m *Model) aiRecordTurnUsage(u ai.Usage) {
	s := m.aiS()
	prof := s.activeProfile()
	agent, slot := "Assistant", ai.SlotMain
	if len(prof.Steps) > 0 {
		if prof.Steps[0].Title != "" {
			agent = prof.Steps[0].Title
		}
		slot = prof.SlotFor(prof.Steps[0])
	}
	kind := s.cfg.Kind
	if cfg, ok := s.aiSlotConfig(slot); ok {
		kind = cfg.Kind
	}
	e := aiUsageEntry{Agent: aiCleanText(agent), Model: u.Model, U: u, Sub: kind == ai.KindClaudeOAuth}
	if e.Model == "" {
		e.Model, _ = s.aiSlotModel(slot)
	}
	if usd, ok := ai.NewPricing(s.settings.Prices).Cost(kind, u); ok {
		e.Cost = &usd
	}
	s.recordUsage(e)
}
