package app

import (
	"strings"
)

// The laptop's record of what the web changed (web design 3.5): one line
// per write op in the Web output channel, with the op, a short summary
// (paths, a label, a count: never file text, chat text or a secret), the
// device and the result. cards.answer keeps its line in the AI channel.

const webAuditChannel = "Web"

// webAuditMax caps a summary; a long list of paths ends in "…".
const webAuditMax = 200

// webAuditOp writes one audit line for a write op of call.
func (m *Model) webAuditOp(call *webCall, summary, result string) {
	m.webAuditLine(call.op, summary, call.conn.device, result)
}

// webAuditLine writes one audit line for op, asked by device.
func (m *Model) webAuditLine(op, summary, device, result string) {
	if device == "" {
		device = "web"
	}
	summary = webName(summary)
	if r := []rune(summary); len(r) > webAuditMax {
		summary = string(r[:webAuditMax-1]) + "…"
	}
	line := webNow().Format("15:04:05") + "  " + op
	if summary != "" {
		line += "  " + summary
	}
	line += "  by=web:" + webName(device) + "  → " + webName(result)
	m.output.Append(webAuditChannel, line)
}

// webAuditPaths is a summary of paths.
func webAuditPaths(paths []string) string { return strings.Join(paths, " ") }
