package app

import (
	"time"

	"github.com/amin-jalali/termocode/internal/agentusage"
	"github.com/amin-jalali/termocode/internal/clock"
)

// agentUsageDemo is AI: Usage with made-up plan windows (DevActionMsg
// "agentusagedemo"; no file is read, no CLI runs): the pill in the
// status bar and the PLANS rows.
func (m *Model) agentUsageDemo() {
	now := clock.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if now.Sub(day) > 9*time.Hour {
		day = day.Add(24 * time.Hour) // keep the made-up resets ahead of the clock
	}
	b := agentusage.New()
	at := day.Add(-time.Hour)
	b.Transcript(agentusage.Key{CLI: "claude"}, "demo-1", 62, day.Add(14*time.Hour+30*time.Minute), 102640, at)
	b.Limit(agentusage.Key{CLI: "codex", Account: "work"}, "", day.Add(15*time.Hour), at)
	b.Transcript(agentusage.Key{CLI: "codex"}, "demo-2", 18, day.Add(20*time.Hour), 20000, at)
	m.agentUsage = b
	m.showAIUsage("")
}
