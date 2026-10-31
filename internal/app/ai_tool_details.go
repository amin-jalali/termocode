package app

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// MCP call details (MCPX-33, UX-11): an MCP tool line in the chat gets a
// "▸ details" button. A click opens the call's full arguments (secrets
// hidden) and its result under the line; another click closes them.
// They are kept in memory for this session only.

const (
	aiDetailMaxBytes = 16 << 10 // of one result
	aiDetailMaxRows  = 60       // per part when open
	aiDetailKeep     = 200      // calls kept
)

type aiToolDetail struct {
	line         string // the tool line's text, so a changed chat does not match
	args, result string
}

// aiMCPTool reports a tool event of an MCP server.
func aiMCPTool(ev ai.Event) bool {
	return strings.HasPrefix(ev.Label, "mcp.") || strings.HasPrefix(ev.Tool, "mcp__")
}

// aiDetailArgs renders a call's arguments for people, secrets hidden.
func aiDetailArgs(args map[string]any) string {
	if len(args) == 0 {
		return "(none)"
	}
	b, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return "(not shown)"
	}
	return ai.RedactSecrets(ai.Redact(string(b)))
}

// aiKeepToolDetail keeps the details of the tool line at index i.
func (s *aiState) aiKeepToolDetail(i int, line string, args map[string]any, result string) {
	if s.toolDetails == nil || len(s.toolDetails) >= aiDetailKeep {
		s.toolDetails, s.toolOpen = map[int]aiToolDetail{}, map[int]bool{}
	}
	if len(result) > aiDetailMaxBytes {
		result = result[:aiDetailMaxBytes] + "\n… (cut)"
	}
	s.toolDetails[i] = aiToolDetail{line: line, args: aiDetailArgs(args), result: ai.RedactSecrets(ai.Redact(result))}
	delete(s.toolOpen, i)
}

// aiToolDetailOf is the details of message i (nil: none).
func (s *aiState) aiToolDetailOf(i int) *aiToolDetail {
	d, ok := s.toolDetails[i]
	if !ok || i >= len(s.conv.Messages) || s.conv.Messages[i].Text != d.line {
		return nil
	}
	return &d
}

// aiToolDetailState is 0 (no details), 1 (closed) or 2 (open): part of
// the transcript cache key.
func (s *aiState) aiToolDetailState(i int) int {
	switch {
	case s.aiToolDetailOf(i) == nil:
		return 0
	case s.toolOpen[i]:
		return 2
	}
	return 1
}

// aiToggleToolDetail opens or closes the details of message i.
func (s *aiState) aiToggleToolDetail(id string) {
	i, err := strconv.Atoi(strings.TrimPrefix(id, "tooldetail:"))
	if err != nil || s.aiToolDetailOf(i) == nil {
		return
	}
	if s.toolOpen == nil {
		s.toolOpen = map[int]bool{}
	}
	s.toolOpen[i] = !s.toolOpen[i]
}

// aiToolDetailLines are the button row and, when open, the details.
func (s *aiState) aiToolDetailLines(i, width int) []aiLine {
	d := s.aiToolDetailOf(i)
	if d == nil {
		return nil
	}
	ind := strings.Repeat(" ", aiIndent+2)
	dim := aiStyle(theme.Fg(theme.TextDim))
	label := "▸ details"
	if s.toolOpen[i] {
		label = "▾ details"
	}
	x := aiIndent + 2
	out := []aiLine{{text: ind + label, style: dim, buttons: []aiLineButton{{x, x + runewidth.StringWidth(label), "tooldetail:" + strconv.Itoa(i)}}}}
	if !s.toolOpen[i] {
		return out
	}
	lim := max(width-aiIndent-5, 8)
	part := func(title, body string) {
		out = append(out, aiLine{text: ind + "  " + title, style: dim})
		rows := strings.Split(aiCleanText(body), "\n")
		more := 0
		if len(rows) > aiDetailMaxRows {
			rows, more = rows[:aiDetailMaxRows], len(rows)-aiDetailMaxRows
		}
		for _, r := range rows {
			if runewidth.StringWidth(r) > lim {
				r = runewidth.Truncate(r, lim, "…")
			}
			out = append(out, aiLine{text: ind + "    " + r, style: dim, sel: aiSel{ok: true, join: "\n"}})
		}
		if more > 0 {
			out = append(out, aiLine{text: ind + "    … " + strconv.Itoa(more) + " more lines", style: dim})
		}
	}
	part("args", d.args)
	part("result", d.result)
	return out
}
