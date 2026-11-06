package agents

import (
	"regexp"
	"strings"
)

// AGX-11: the usage-limit messages of the agents on their own screen
// (samples in testdata/limit/). Only a line that starts with the CLI's
// own wording counts, so a model answer that talks about limits does
// not. The reset words are read by clistep.LimitReset.
var limitTable = map[string][]*regexp.Regexp{
	"claude": res(`(?i)^\W*(claude (ai )?usage limit reached|\d+-hour limit reached|weekly limit reached|opus limit reached|you've hit your (usage )?limit)`),
	"codex":  res(`(?i)^\W*(you've hit your usage limit|usage limit (reached|exceeded))`),
}

// HasLimitRules says whether LimitLine can read agent id's screen.
func HasLimitRules(id string) bool {
	_, ok := limitTable[id]
	return ok
}

// LimitLine is the usage-limit line on the bottom of agent id's screen
// (the same bottom lines as Detect), trimmed.
func LimitLine(id string, lines []string) (string, bool) {
	rs, ok := limitTable[id]
	if !ok {
		return "", false
	}
	for _, l := range bottom(lines) {
		for _, re := range rs {
			if re.MatchString(l) {
				return strings.TrimSpace(strings.TrimLeft(l, " \t│|⎿■•")), true
			}
		}
	}
	return "", false
}

// IsPrompt says whether the screen asks the user something: a permission
// prompt of the agent or a y/n question of any program.
func IsPrompt(id string, lines []string) bool {
	tail := bottom(lines)
	rs := anyWaiting
	if r, ok := table[id]; ok {
		rs = append(append([]*regexp.Regexp(nil), r.waiting...), anyWaiting...)
	}
	for _, l := range tail {
		for _, re := range rs {
			if re.MatchString(l) {
				return true
			}
		}
	}
	return false
}
