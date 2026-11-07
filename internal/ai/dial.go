package ai

import (
	"fmt"
	"sort"
	"strings"
)

// Effort dial (CMP-24): one simple knob for "how hard should the AI
// work". Each mode bundles an agent profile and a reasoning level:
//
//	low    profile fast,    think low
//	medium profile default, think medium
//	high   profile default, think high
//	ultra  profile default, think max
//
// config.json "ai_dial" changes a mode: {"ultra": {"profile":
// "until-done"}}; a field that is set replaces only that field. The
// dial never hides anything: the chat header still shows the profile
// chip and the "think <level>" chip it set.

// The dial modes.
const (
	DialLow    = "low"
	DialMedium = "medium"
	DialHigh   = "high"
	DialUltra  = "ultra"
)

// DialModes are the modes, least effort first.
var DialModes = []string{DialLow, DialMedium, DialHigh, DialUltra}

// DialSetting is one mode of the dial: the profile it switches to (""
// keeps the active one) and the chat's reasoning level ("" the
// model's own).
type DialSetting struct {
	Profile   string `json:"profile,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

var defaultDial = map[string]DialSetting{
	DialLow:    {Profile: ProfileFast, Reasoning: ReasoningLow},
	DialMedium: {Profile: ProfileDefault, Reasoning: ReasoningMedium},
	DialHigh:   {Profile: ProfileDefault, Reasoning: ReasoningHigh},
	DialUltra:  {Profile: ProfileDefault, Reasoning: ReasoningMax},
}

// NormalizeDialMode is mode lower-case and trimmed; "" when it is not a
// mode.
func NormalizeDialMode(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	if _, ok := defaultDial[m]; !ok {
		return ""
	}
	return m
}

// EffortDial is the dial with ai_dial applied. An unknown mode or a bad
// reasoning level is skipped and named in problems (the built-in value
// stays).
func (s Settings) EffortDial() (map[string]DialSetting, []string) {
	out := make(map[string]DialSetting, len(defaultDial))
	for k, v := range defaultDial {
		out[k] = v
	}
	var problems []string
	keys := make([]string, 0, len(s.Dial))
	for k := range s.Dial {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := s.Dial[k]
		mode := NormalizeDialMode(k)
		if mode == "" {
			problems = append(problems, fmt.Sprintf("config.json: ai_dial has no mode %q (%s)", k, strings.Join(DialModes, ", ")))
			continue
		}
		cur := out[mode]
		if p := strings.TrimSpace(v.Profile); p != "" {
			cur.Profile = p
		}
		if strings.TrimSpace(v.Reasoning) != "" {
			if err := CheckReasoning(v.Reasoning); err != nil {
				problems = append(problems, "config.json: ai_dial."+mode+": "+err.Error())
			} else {
				cur.Reasoning = NormalizeReasoning(v.Reasoning)
			}
		}
		out[mode] = cur
	}
	return out, problems
}

// NextDialMode is the mode after cur: none → low → … → ultra → low.
func NextDialMode(cur string) string {
	cur = NormalizeDialMode(cur)
	for i, m := range DialModes {
		if m == cur {
			return DialModes[(i+1)%len(DialModes)]
		}
	}
	return DialModes[0]
}

// DialModeOf is the mode whose profile and level are profile and
// reasoning ("" when none matches: the user set them some other way).
// A mode that keeps the profile matches any profile.
func DialModeOf(dial map[string]DialSetting, profile, reasoning string) string {
	reasoning = NormalizeReasoning(reasoning)
	for _, m := range DialModes {
		d, ok := dial[m]
		if !ok || NormalizeReasoning(d.Reasoning) != reasoning {
			continue
		}
		if d.Profile == "" || d.Profile == profile {
			return m
		}
	}
	return ""
}

// DialSummary says what a mode does: "profile default · think max".
func DialSummary(d DialSetting) string {
	prof := "profile unchanged"
	if d.Profile != "" {
		prof = "profile " + d.Profile
	}
	think := "the model's own thinking"
	if l := NormalizeReasoning(d.Reasoning); l != "" {
		think = "think " + l
	}
	return prof + " · " + think
}
