package webproto

import "time"

// Now (whose turn, why, which rule). now.act puts one inbox item away or
// takes the last such step back: done, later (free text: "in 2 hours",
// "tomorrow 9:00", "next week"), mute (the item, or with Repo its whole
// repo, until something asks the user), undo. It only hides; it never
// acts on the item's source. now.rules answers the first-run questions
// (Setup, or Skip: the defaults stay) or replaces the rule rows (at most
// MaxNowRules). The inbox topic shows the result.

// Now ops.
const (
	OpNowAct   = "now.act"
	OpNowRules = "now.rules"
)

// Now limits.
const (
	MaxNowRules    = 7
	maxNowWhen     = 40
	maxNowRuleID   = 32
	maxNowRuleName = 40
	maxNowSentence = 120
	maxNowWords    = 10
)

// NowActArgs is one step: Do is done, later, mute or undo; ID is an
// inbox item's id (not for undo); When is Later's free text; Repo mutes
// the item's repo instead of the item.
type NowActArgs struct {
	Do   string `json:"do" enum:"done|later|mute|undo"`
	ID   string `json:"id,omitempty"`
	WS   string `json:"ws,omitempty"`
	When string `json:"when,omitempty"`
	Repo bool   `json:"repo,omitempty"`
}

func (a *NowActArgs) validate(int) error {
	switch a.Do {
	case "undo":
		if a.ID != "" || a.When != "" {
			return invalid("undo takes nothing")
		}
		return nil
	case "done", "later", "mute":
	default:
		return invalid("do is done, later, mute or undo")
	}
	switch {
	case !validName(a.ID, 2*MaxInboxTitle):
		return invalid("bad item id")
	case a.WS != "" && !InboxWS(a.WS):
		return invalid("bad ws")
	case a.Do == "later" && !validName(a.When, maxNowWhen):
		return invalid("say when")
	case a.Do != "later" && a.When != "":
		return invalid("when is for later")
	case a.Do != "mute" && a.Repo:
		return invalid("repo is for mute")
	}
	return nil
}

// NowActResult says what was put away (or brought back by undo) and,
// for later, when it comes back (Unix ms).
type NowActResult struct {
	ID    string `json:"id"`
	Until int64  `json:"until,omitempty"`
}

// NowAnswers are the 4 first-run questions: what goes on top (signals),
// where from (sources), how many on Home (3, 5 or 10), bots never on top.
type NowAnswers struct {
	Top      []string `json:"top"`
	From     []string `json:"from"`
	Show     int      `json:"show"`
	HideBots bool     `json:"hide_bots"`
}

// NowRulesArgs is exactly one of: Setup (the answers), Skip (keep the
// defaults, never ask again), Rules (the new rule rows, in order).
type NowRulesArgs struct {
	Setup *NowAnswers `json:"setup,omitempty"`
	Skip  bool        `json:"skip,omitempty"`
	Rules []NowRule   `json:"rules,omitempty"`
}

func (a *NowRulesArgs) validate(int) error {
	n := 0
	if a.Setup != nil {
		n++
	}
	if a.Skip {
		n++
	}
	if a.Rules != nil {
		n++
	}
	if n != 1 {
		return invalid("give one of setup, skip, rules")
	}
	if s := a.Setup; s != nil {
		if s.Show != 3 && s.Show != 5 && s.Show != 10 {
			return invalid("show is 3, 5 or 10")
		}
		if !nowWords(s.Top) || !nowWords(s.From) {
			return invalid("bad answer")
		}
	}
	if a.Rules != nil {
		if len(a.Rules) == 0 || len(a.Rules) > MaxNowRules {
			return invalid("1 to %d rules", MaxNowRules)
		}
		for _, r := range a.Rules {
			if !validName(r.ID, maxNowRuleID) || !validName(r.Name, maxNowRuleName) || len(r.Sentence) > maxNowSentence || hasControl(r.Sentence) ||
				len(r.Signal) > maxNowRuleID || hasControl(r.Signal) || r.Level < 0 || r.Level > 4 {
				return invalid("bad rule")
			}
		}
	}
	return nil
}

func nowWords(xs []string) bool {
	if len(xs) > maxNowWords {
		return false
	}
	for _, x := range xs {
		if !validName(x, maxNowRuleID) {
			return false
		}
	}
	return true
}

var opNowRows = []Policy{
	{Op: OpNowAct, New: newOf[NowActArgs](), Role: RoleController, Rate: Rate{N: 5, Per: time.Second}, MaxBytes: 2 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpNowRules, New: newOf[NowRulesArgs](), Role: RoleController, Rate: Rate{N: 2, Per: time.Second}, MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opNowRows...); return true }()

func init() {
	registerResult(OpNowAct, NowActResult{})
	registerResult(OpNowRules, NowInfo{})
}
