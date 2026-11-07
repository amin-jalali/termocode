package agents

import (
	"regexp"
	"strconv"
	"strings"
)

// Choice is one numbered answer of the question an agent shows.
type Choice struct {
	N      int
	Label  string
	Cursor bool // the menu's cursor is on it
}

// choiceRow is a menu row: an optional box border, an optional cursor
// (❯ Claude Code, › Codex), "N." and the label.
var choiceRow = regexp.MustCompile(`^\s*[│|]?\s*([❯›>])?\s*(\d{1,2})\.\s+(.*?)\s*[│|]?\s*$`)

// choiceHint is a key hint at the end of a label: "(esc)", "(y)".
var choiceHint = regexp.MustCompile(`\s*\((?:esc|[a-z])\)$`)

// menuAgents are the agents whose questions are numbered menus.
var menuAgents = map[string]bool{"claude": true, "codex": true}

// maxChoiceLabel is the longest label kept (in runes).
const maxChoiceLabel = 120

// Choices reads the numbered menu at the bottom of agent id's screen,
// in order (1 first). It is nil unless the screen asks (Detect is
// Waiting) and the menu starts at 1 with at least two rows. Rows under
// an option that carry no number (its description) are skipped.
func Choices(id string, lines []string) []Choice {
	if !menuAgents[id] || Detect(id, lines) != Waiting {
		return nil
	}
	var out []Choice // newest (highest number) first
	for _, l := range bottom(lines) {
		m := choiceRow.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		if len(out) > 0 && n != out[len(out)-1].N-1 {
			break // another list above the menu
		}
		label := choiceHint.ReplaceAllString(strings.TrimSpace(m[3]), "")
		if r := []rune(label); len(r) > maxChoiceLabel {
			label = string(r[:maxChoiceLabel-1]) + "…"
		}
		out = append(out, Choice{N: n, Label: label, Cursor: m[1] != ""})
		if n == 1 {
			break
		}
	}
	if len(out) < 2 || out[len(out)-1].N != 1 {
		return nil
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
