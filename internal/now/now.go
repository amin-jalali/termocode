// Package now is the model behind the Now list: whose turn an item is,
// why it is there, which of the user's rules placed it, the default
// ranking and the user's Done / Later / Mute choices.
//
// Everything here is plain and visible: the turn rules are three lines
// (my action removes me, the other side's action adds me, bots never take
// a turn except a red result on my branch), a rule is a short row the user
// can read and switch off, and the ranking is a fixed order (your turn,
// level P1→P4, due, age). Nothing here acts: it only sorts and hides.
package now

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Turn values: whose move an item waits for.
const (
	TurnMe     = "me"
	TurnOthers = "others"
	TurnNone   = "none"
)

// Signals: what kind of ask an item is. A rule matches one signal.
const (
	SigAgent   = "agent"   // an agent waits for my OK
	SigCIRed   = "ci_red"  // CI fails on my branch
	SigReview  = "review"  // someone asks me to review
	SigDue     = "due"     // a tracker task is due today or late
	SigMention = "mention" // someone @-mentions me
)

// Sources the user can pick in "Where from?".
var Sources = []string{"github", "linear", "jira", "youtrack", "agents", "ci", "artifacts"}

// WhyBack is the reason of an item back from Later.
const WhyBack = "Back from Later"

// MaxRules caps the user's rules.
const MaxRules = 7

// Actor is who did something on an item.
type Actor struct {
	Name string
	Me   bool
	Bot  bool
}

// Event is one action on an item. Red marks a red result (a failed check)
// on my branch: the one bot event that gives me the turn.
type Event struct {
	Who Actor
	At  time.Time
	Red bool
}

// Attention is whose turn an item is after its events, the people it
// waits on (when it is theirs), and why and since when.
type Attention struct {
	Turn    string
	Waiting []string
	Why     string
	At      time.Time
}

// Walk applies the turn rules to evs in order.
func Walk(evs []Event) Attention {
	a := Attention{Turn: TurnNone}
	var seen []string
	for _, e := range evs {
		switch {
		case e.Who.Bot:
			if e.Red {
				a = Attention{Turn: TurnMe, Why: "red result", At: e.At}
			}
		case e.Who.Me:
			a = Attention{Turn: TurnOthers, Waiting: append([]string(nil), seen...), At: e.At}
		default:
			if !contains(seen, e.Who.Name) {
				seen = append(seen, e.Who.Name)
			}
			a = Attention{Turn: TurnMe, Why: e.Who.Name + " asked you", At: e.At}
		}
	}
	return a
}

// Rule is one row of the user's rules: an item with Signal goes on top at
// Level (1..4) while On. A rule without a signal (No bots) keeps bots off
// the top.
type Rule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Sentence string `json:"sentence"`
	Signal   string `json:"signal,omitempty"`
	Level    int    `json:"level"`
	On       bool   `json:"on"`
}

// RuleNoBots is the No bots row's id.
const RuleNoBots = "nobots"

// DefaultRules are the first-run rule rows.
func DefaultRules() []Rule {
	return []Rule{
		{ID: "agents", Name: "Agents", Sentence: "An agent waits for my OK", Signal: SigAgent, Level: 1, On: true},
		{ID: "redci", Name: "Red CI", Sentence: "CI fails on my branch", Signal: SigCIRed, Level: 1, On: true},
		{ID: "reviews", Name: "Reviews", Sentence: "Someone asks me to review", Signal: SigReview, Level: 2, On: true},
		{ID: "due", Name: "Due today", Sentence: "Tracker task due today or late", Signal: SigDue, Level: 2, On: true},
		{ID: RuleNoBots, Name: "No bots", Sentence: "Bots never take my turn", On: true},
		{ID: "mentions", Name: "Mentions", Sentence: "Someone @-mentions me", Signal: SigMention, Level: 3, On: false},
	}
}

// LevelName is "P1".."P4", or "–" for a rule without a level.
func LevelName(l int) string {
	if l < 1 || l > 4 {
		return "–"
	}
	return fmt.Sprintf("P%d", l)
}

// ValidRule reports what is wrong with r ("" when nothing).
func ValidRule(r Rule) error {
	switch {
	case r.ID == "" || len(r.ID) > 32:
		return errors.New("a rule needs a short id")
	case r.Name == "" || len(r.Name) > 40:
		return errors.New("a rule needs a short name")
	case len(r.Sentence) > 120:
		return errors.New("the rule's sentence is too long")
	case r.ID != RuleNoBots && (r.Level < 1 || r.Level > 4):
		return errors.New("the level is P1 to P4")
	}
	return nil
}

// AddRule appends r, keeping at most MaxRules rows with unique ids.
func AddRule(rules []Rule, r Rule) ([]Rule, error) {
	if len(rules) >= MaxRules {
		return rules, fmt.Errorf("at most %d rules", MaxRules)
	}
	if err := ValidRule(r); err != nil {
		return rules, err
	}
	for _, x := range rules {
		if x.ID == r.ID {
			return rules, errors.New("a rule with that id is there")
		}
	}
	return append(append([]Rule(nil), rules...), r), nil
}

// Answers are the 4 first-run questions: what goes on top (signals),
// where from (sources), how many on Home (3, 5 or 10), and whether bots
// are never on top.
type Answers struct {
	Top      []string `json:"top"`
	From     []string `json:"from"`
	Show     int      `json:"show"`
	HideBots bool     `json:"hide_bots"`
}

// DefaultAnswers are the answers already set on the first-run card.
func DefaultAnswers() Answers {
	return Answers{Top: []string{SigAgent, SigCIRed, SigReview}, From: []string{"github", "linear", "agents", "ci"}, Show: 5, HideBots: true}
}

// ShowN is how many Your-turn items Home shows: 3, 5 or 10.
func (a Answers) ShowN() int {
	switch a.Show {
	case 3, 10:
		return a.Show
	}
	return 5
}

// RulesFor makes the rule rows from the answers: the default rows, on
// when their signal was picked for the top.
func RulesFor(a Answers) []Rule {
	r := DefaultRules()
	for i := range r {
		if r[i].ID == RuleNoBots {
			r[i].On = a.HideBots
			continue
		}
		r[i].On = contains(a.Top, r[i].Signal)
	}
	return r
}

// Item is one thing the Now list shows. Since is when it started; WhyAt
// when its current reason started (a newer one brings a done item back).
type Item struct {
	ID      string
	Kind    string
	Source  string
	Repo    string
	Title   string
	Since   time.Time
	Turn    string
	Waiting []string
	Signal  string
	Bot     bool
	Due     time.Time
	Head    string
	Why     string
	WhyAt   time.Time
	Rule    string
	Level   int
}

// Prefs is what places items: the rules in order, and the picked sources
// (empty: all).
type Prefs struct {
	Rules []Rule
	From  []string
}

// Place sets it's rule and level: a bot under an on No bots row is never
// on top and takes no turn; a source the user did not pick is P4; else
// the first on rule with the item's signal wins; no rule is P4.
func Place(it *Item, p Prefs) {
	it.Rule, it.Level = "", 4
	if it.Bot {
		for _, r := range p.Rules {
			if r.ID == RuleNoBots && r.On {
				it.Rule, it.Turn = RuleNoBots, TurnNone
				return
			}
		}
	}
	if len(p.From) > 0 && it.Source != "" && !contains(p.From, it.Source) {
		return
	}
	for _, r := range p.Rules {
		if r.On && r.Signal != "" && r.Signal == it.Signal {
			it.Rule, it.Level = r.ID, r.Level
			return
		}
	}
}

var turnOrder = map[string]int{TurnMe: 0, TurnOthers: 1, TurnNone: 2}

// Rank sorts items: your turn, waiting, the rest; inside, level, then a
// due date by today's end first (earlier first), then any due date, then
// age (oldest first), then id.
func Rank(items []Item, now time.Time) {
	eod := endOfDay(now)
	dueRank := func(it Item) int {
		switch {
		case it.Due.IsZero():
			return 2
		case !it.Due.After(eod):
			return 0
		}
		return 1
	}
	lvl := func(l int) int {
		if l < 1 || l > 4 {
			return 4
		}
		return l
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if x, y := turnOrder[a.Turn], turnOrder[b.Turn]; x != y {
			return x < y
		}
		if x, y := lvl(a.Level), lvl(b.Level); x != y {
			return x < y
		}
		if x, y := dueRank(a), dueRank(b); x != y {
			return x < y
		}
		if !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		if !a.Since.Equal(b.Since) {
			return a.Since.Before(b.Since)
		}
		return a.ID < b.ID
	})
}

// View is the Now list in sections.
type View struct {
	Turn    []Item
	Overdue []Item
	Waiting []Item
	Other   []Item
}

// Split ranks items and puts them in sections: overdue (due before
// today) apart, then by turn.
func Split(items []Item, now time.Time) View {
	xs := append([]Item(nil), items...)
	Rank(xs, now)
	sod := startOfDay(now)
	var v View
	for _, it := range xs {
		switch {
		case !it.Due.IsZero() && it.Due.Before(sod) && it.Turn != TurnNone:
			v.Overdue = append(v.Overdue, it)
		case it.Turn == TurnMe:
			v.Turn = append(v.Turn, it)
		case it.Turn == TurnOthers:
			v.Waiting = append(v.Waiting, it)
		default:
			v.Other = append(v.Other, it)
		}
	}
	return v
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func endOfDay(t time.Time) time.Time { return startOfDay(t).AddDate(0, 0, 1).Add(-time.Nanosecond) }

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
