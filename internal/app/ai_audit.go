package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// AI audit log (Group R4): every agent tool call is one line in the
// Output panel's "AI" channel (View: Output, then the AI chip):
//
//	14:03:22  write_file  path=a.go content=<12 lines>  gate=review  by=user (review)  → The user accepted 1 of 2 changes…
//
// gate is how the call was cleared (auto, confirm, review, invalid), by
// who decided (auto, user allowed / denied, auto-approved (class), user
// (review)), then the first line of the result.

// aiAuditChannel is the Output channel of the audit log.
const aiAuditChannel = "AI"

// aiAuditPending is a started tool call waiting for its result.
type aiAuditPending struct {
	at   time.Time
	tool string
	args string
}

// aiAuditStart remembers a tool call start.
func (s *aiState) aiAuditStart(ev ai.Event) {
	s.auditPending = &aiAuditPending{at: clock.Now(), tool: aiAuditTool(ev), args: aiAuditArgs(ev.Args)}
}

// aiAuditTool is the tool name of an audit line: the display name
// (mcp.linear.create_issue) when the tool has one.
func aiAuditTool(ev ai.Event) string {
	if ev.Label != "" {
		return ev.Label
	}
	return ev.Tool
}

// aiAuditResult writes the line of a finished tool call.
func (m *Model) aiAuditResult(ev ai.Event) {
	s := m.aiS()
	p := s.auditPending
	s.auditPending = nil
	if p == nil || p.tool != aiAuditTool(ev) {
		p = &aiAuditPending{at: clock.Now(), tool: aiAuditTool(ev)}
	}
	m.aiAudit(p.at, p.tool, p.args, ev.Gate, ev.Decision, ev.Detail)
}

// aiAudit appends one audit line.
func (m *Model) aiAudit(at time.Time, tool, args, gate, by, result string) {
	if gate == "" {
		gate = "-"
	}
	if by == "" {
		by = "-"
	}
	line := at.Format("15:04:05") + "  " + tool
	if args != "" {
		line += "  " + args
	}
	line += "  gate=" + gate + "  by=" + by + "  → " + aiAuditResultText(result)
	m.output.Append(aiAuditChannel, line)
}

// aiAuditArgs renders call arguments short: key=value, sorted; long or
// multi-line values are summarised.
func aiAuditArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+aiAuditValue(args[k]))
	}
	return strings.Join(parts, " ")
}

func aiAuditValue(v any) string {
	s, ok := v.(string)
	if !ok {
		s = fmt.Sprint(v)
	}
	if n := strings.Count(strings.TrimSuffix(s, "\n"), "\n"); n > 0 {
		return fmt.Sprintf("<%d lines, %d B>", n+1, len(s))
	}
	if r := []rune(s); len(r) > 40 {
		return fmt.Sprintf("%q", string(r[:39])+"…")
	}
	if s == "" || strings.ContainsAny(s, " \t\"") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// aiAuditResultText is the first line of a result, capped.
func aiAuditResultText(s string) string {
	s = strings.TrimSpace(ai.Redact(s))
	first, rest, more := strings.Cut(s, "\n")
	if r := []rune(first); len(r) > 120 {
		first = string(r[:119]) + "…"
	}
	if more && strings.TrimSpace(rest) != "" {
		first += fmt.Sprintf(" (+%d lines)", strings.Count(rest, "\n")+1)
	}
	return first
}
