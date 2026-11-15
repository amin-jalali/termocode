package now

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

var (
	me   = Actor{Name: "you", Me: true}
	sara = Actor{Name: "Sara"}
	ali  = Actor{Name: "Ali"}
	bot  = Actor{Name: "ci-bot", Bot: true}
)

// Spec (Now, attention set): my action removes me, the other side's
// action adds me, bots never take a turn except a red result on my
// branch. Waiting names the people the turn went to.
func TestWalkWhoseTurn(t *testing.T) {
	cases := []struct {
		name    string
		evs     []Event
		turn    string
		waiting []string
		why     string
		when    time.Time
	}{
		{"nothing happened", nil, TurnNone, nil, "", time.Time{}},
		{"someone asks me", []Event{{Who: sara, At: at(1)}}, TurnMe, nil, "Sara asked you", at(1)},
		{"my reply removes me", []Event{{Who: sara, At: at(1)}, {Who: me, At: at(2)}}, TurnOthers, []string{"Sara"}, "", at(2)},
		{"their reply adds me back", []Event{{Who: sara, At: at(1)}, {Who: me, At: at(2)}, {Who: ali, At: at(3)}}, TurnMe, nil, "Ali asked you", at(3)},
		{"waiting names everyone else, once", []Event{{Who: sara, At: at(1)}, {Who: ali, At: at(2)}, {Who: sara, At: at(3)}, {Who: me, At: at(4)}}, TurnOthers, []string{"Sara", "Ali"}, "", at(4)},
		{"a bot never takes a turn", []Event{{Who: sara, At: at(1)}, {Who: me, At: at(2)}, {Who: bot, At: at(3)}}, TurnOthers, []string{"Sara"}, "", at(2)},
		{"a bot alone gives nobody a turn", []Event{{Who: bot, At: at(1)}}, TurnNone, nil, "", time.Time{}},
		{"a red result on my branch is my turn", []Event{{Who: me, At: at(1)}, {Who: bot, At: at(2), Red: true}}, TurnMe, nil, "red result", at(2)},
	}
	for _, c := range cases {
		got := Walk(c.evs)
		if got.Turn != c.turn || !reflect.DeepEqual(got.Waiting, c.waiting) || got.Why != c.why || !got.At.Equal(c.when) {
			t.Errorf("%s: got %+v, want turn %q waiting %v why %q at %v", c.name, got, c.turn, c.waiting, c.why, c.when)
		}
	}
}

// Spec: the first-run defaults are the visible rule rows: Agents P1,
// Red CI P1, Reviews P2, Due today P2, No bots on, Mentions P3 off. At
// most MaxRules (7) rows.
func TestDefaultRules(t *testing.T) {
	r := DefaultRules()
	var got []string
	for _, x := range r {
		got = append(got, x.ID+":"+x.Name+":"+LevelName(x.Level)+":"+onOff(x.On))
		if x.Sentence == "" {
			t.Errorf("rule %s has no sentence", x.ID)
		}
	}
	want := []string{"agents:Agents:P1:on", "redci:Red CI:P1:on", "reviews:Reviews:P2:on", "due:Due today:P2:on", "nobots:No bots:–:on", "mentions:Mentions:P3:off"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults:\n got %v\nwant %v", got, want)
	}
	if MaxRules != 7 {
		t.Fatalf("MaxRules = %d, want 7", MaxRules)
	}
	more := r
	for i := 0; len(more) < MaxRules; i++ {
		var err error
		more, err = AddRule(more, Rule{ID: "x" + string(rune('a'+i)), Name: "X", Sentence: "x", Signal: SigMention, Level: 3, On: true})
		if err != nil {
			t.Fatalf("add rule %d: %v", len(more)+1, err)
		}
	}
	if _, err := AddRule(more, Rule{ID: "eight", Name: "8", Sentence: "x", Signal: SigMention, Level: 3}); err == nil {
		t.Fatal("an eighth rule was added")
	}
	if _, err := AddRule(r, Rule{ID: "agents", Name: "Again", Sentence: "x", Signal: SigAgent, Level: 1}); err == nil {
		t.Fatal("a second rule with the same id was added")
	}
	if _, err := AddRule(r, Rule{ID: "bad", Name: "Bad", Sentence: "x", Signal: SigAgent, Level: 9}); err == nil {
		t.Fatal("a rule with level 9 was added")
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// Spec: the 4 first-run questions make the rule rows: the checked "on
// top" kinds are on, the others off; "never on top: bots" is the No bots
// row; Show is 3, 5 or 10 (5 when unset or odd).
func TestRulesFromAnswers(t *testing.T) {
	a := Answers{Top: []string{SigAgent, SigMention}, From: []string{"github", "agents"}, Show: 3, HideBots: false}
	r := RulesFor(a)
	on := map[string]bool{}
	for _, x := range r {
		on[x.ID] = x.On
	}
	want := map[string]bool{"agents": true, "redci": false, "reviews": false, "due": false, "nobots": false, "mentions": true}
	if !reflect.DeepEqual(on, want) {
		t.Fatalf("rules from answers: got %v, want %v", on, want)
	}
	if d := DefaultAnswers(); d.Show != 5 || !d.HideBots || len(d.Top) != 3 {
		t.Fatalf("default answers: %+v", d)
	}
	for in, want := range map[int]int{0: 5, 3: 3, 5: 5, 10: 10, 7: 5} {
		if got := (Answers{Show: in}).ShowN(); got != want {
			t.Errorf("Show %d → %d, want %d", in, got, want)
		}
	}
}

// Spec: the top rule (first, on) whose signal matches places the item
// and sets its level; a bot item goes under No bots (never on top, no
// turn); a source the user did not pick is never on top; no rule: P4.
func TestPlace(t *testing.T) {
	p := Prefs{Rules: DefaultRules(), From: []string{"github", "agents", "ci"}}
	cases := []struct {
		it    Item
		rule  string
		level int
		turn  string
	}{
		{Item{ID: "a", Signal: SigAgent, Source: "agents", Turn: TurnMe}, "agents", 1, TurnMe},
		{Item{ID: "b", Signal: SigCIRed, Source: "ci", Turn: TurnMe}, "redci", 1, TurnMe},
		{Item{ID: "c", Signal: SigReview, Source: "github", Turn: TurnMe}, "reviews", 2, TurnMe},
		{Item{ID: "d", Signal: SigMention, Source: "github", Turn: TurnMe}, "", 4, TurnMe},
		{Item{ID: "e", Signal: SigReview, Source: "github", Turn: TurnMe, Bot: true}, "nobots", 4, TurnNone},
		{Item{ID: "f", Signal: SigDue, Source: "jira", Turn: TurnMe}, "", 4, TurnMe},
	}
	for _, c := range cases {
		it := c.it
		Place(&it, p)
		if it.Rule != c.rule || it.Level != c.level || it.Turn != c.turn {
			t.Errorf("%s: rule %q level %d turn %q, want %q %d %q", c.it.ID, it.Rule, it.Level, it.Turn, c.rule, c.level, c.turn)
		}
	}
	// A rule turned off does not place; the next one may.
	r := DefaultRules()
	r[0].On = false
	it := Item{ID: "a", Signal: SigAgent, Source: "agents", Turn: TurnMe}
	Place(&it, Prefs{Rules: r})
	if it.Rule != "" || it.Level != 4 {
		t.Errorf("agents off: rule %q level %d", it.Rule, it.Level)
	}
	// No From list: every source counts.
	it = Item{ID: "f", Signal: SigDue, Source: "jira", Turn: TurnMe}
	Place(&it, Prefs{Rules: DefaultRules()})
	if it.Rule != "due" || it.Level != 2 {
		t.Errorf("no From: rule %q level %d", it.Rule, it.Level)
	}
}

// Spec (ranking, deterministic): your turn first, then waiting, then the
// rest; inside, level P1→P4, then due (overdue/today first), then age
// (oldest first), then id.
func TestRank(t *testing.T) {
	now := t0
	items := []Item{
		{ID: "none", Turn: TurnNone, Level: 1, Since: at(-100)},
		{ID: "wait", Turn: TurnOthers, Level: 1, Since: at(-100)},
		{ID: "p2-new", Turn: TurnMe, Level: 2, Since: at(-1)},
		{ID: "p2-old", Turn: TurnMe, Level: 2, Since: at(-50)},
		{ID: "p2-due", Turn: TurnMe, Level: 2, Since: at(-1), Due: now.Add(2 * time.Hour)},
		{ID: "p1", Turn: TurnMe, Level: 1, Since: at(-1)},
		{ID: "p4", Turn: TurnMe, Level: 4, Since: at(-500)},
		{ID: "p2-later-due", Turn: TurnMe, Level: 2, Since: at(-60), Due: now.Add(72 * time.Hour)},
	}
	Rank(items, now)
	var got []string
	for _, x := range items {
		got = append(got, x.ID)
	}
	want := []string{"p1", "p2-due", "p2-later-due", "p2-old", "p2-new", "p4", "wait", "none"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rank:\n got %v\nwant %v", got, want)
	}
}

// Spec: Split gives the Now sections: your turn, overdue (due before
// today, apart), waiting on others, everything else.
func TestSplit(t *testing.T) {
	now := t0
	items := []Item{
		{ID: "a", Turn: TurnMe, Level: 1},
		{ID: "late", Turn: TurnMe, Level: 2, Due: now.Add(-48 * time.Hour)},
		{ID: "w", Turn: TurnOthers, Level: 2},
		{ID: "x", Turn: TurnNone, Level: 4},
		{ID: "today", Turn: TurnMe, Level: 2, Due: now.Add(time.Hour)},
	}
	v := Split(items, now)
	ids := func(xs []Item) (o []string) {
		for _, x := range xs {
			o = append(o, x.ID)
		}
		return
	}
	if got := ids(v.Turn); !reflect.DeepEqual(got, []string{"a", "today"}) {
		t.Errorf("turn %v", got)
	}
	if got := ids(v.Overdue); !reflect.DeepEqual(got, []string{"late"}) {
		t.Errorf("overdue %v", got)
	}
	if got := ids(v.Waiting); !reflect.DeepEqual(got, []string{"w"}) {
		t.Errorf("waiting %v", got)
	}
	if got := ids(v.Other); !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("other %v", got)
	}
}

// Spec: "h later" takes free text: "in 2 hours", "in 30 min", "tomorrow"
// (9:00), "tomorrow 14:30", "next week" (Monday 9:00), "tonight" (18:00).
func TestParseLater(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) // a Sunday
	cases := map[string]time.Time{
		"in 2 hours":     now.Add(2 * time.Hour),
		"in 1 hour":      now.Add(time.Hour),
		"2h":             now.Add(2 * time.Hour),
		"in 30 min":      now.Add(30 * time.Minute),
		"tomorrow":       time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC),
		"Tomorrow 9:00":  time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC),
		"tomorrow 14:30": time.Date(2026, 11, 16, 14, 30, 0, 0, time.UTC),
		"next week":      time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC),
		"tonight":        time.Date(2026, 11, 15, 18, 0, 0, 0, time.UTC),
		"in 3 days":      now.Add(72 * time.Hour),
	}
	for in, want := range cases {
		got, err := ParseLater(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q → %v %v, want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "soon", "in -2 hours", "in 99999 days", "tomorrow 25:00"} {
		if _, err := ParseLater(bad, now); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// Spec: Done / Later / Mute are kept on disk (per user, not per project)
// with one-step undo; a done item comes back when something new happens
// on it; a later one comes back on top at its time; a mute holds until
// someone asks me (review or mention); done is kept 30 days.
func TestStoreDoneLaterMuteUndo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "now.json")
	s := Open(path)
	if s.State().Setup {
		t.Fatal("a new store says the questions were answered")
	}
	if got := len(s.State().Rules); got != len(DefaultRules()) {
		t.Fatalf("a new store has %d rules, want the defaults", got)
	}
	a := Item{ID: "ci:main", Turn: TurnMe, WhyAt: at(0), Signal: SigCIRed}
	b := Item{ID: "pr:412", Turn: TurnMe, WhyAt: at(0), Signal: SigReview, Repo: "shop"}
	c := Item{ID: "pr:401", Turn: TurnMe, WhyAt: at(0), Signal: SigCIRed, Repo: "shop"}

	if err := s.Done(a.ID, at(1)); err != nil {
		t.Fatal(err)
	}
	if !s.Hidden(a, at(2)) {
		t.Fatal("a done item still shows")
	}
	newer := a
	newer.WhyAt = at(5)
	if s.Hidden(newer, at(6)) {
		t.Fatal("a done item with something new on it stays hidden")
	}
	if id, ok := s.Undo(); !ok || id != a.ID {
		t.Fatalf("undo: %q %v", id, ok)
	}
	if s.Hidden(a, at(2)) {
		t.Fatal("undo did not bring the item back")
	}

	if err := s.Later(b.ID, at(120)); err != nil {
		t.Fatal(err)
	}
	if !s.Hidden(b, at(60)) {
		t.Fatal("a later item shows before its time")
	}
	if s.Hidden(b, at(121)) {
		t.Fatal("a later item stays hidden after its time")
	}
	got := s.Apply([]Item{b}, at(121))
	if len(got) != 1 || got[0].Turn != TurnMe || got[0].Why != WhyBack {
		t.Fatalf("a later item is not back on top: %+v", got)
	}

	if err := s.Mute(MuteKey(c, false), at(1)); err != nil {
		t.Fatal(err)
	}
	if !s.Hidden(c, at(2)) {
		t.Fatal("a muted item shows")
	}
	asks := c
	asks.Signal = SigReview
	if s.Hidden(asks, at(3)) {
		t.Fatal("a muted item stays hidden when someone asks me")
	}
	if err := s.Mute(MuteKey(b, true), at(1)); err != nil {
		t.Fatal(err)
	}
	if k := MuteKey(b, true); k != "repo:shop" {
		t.Fatalf("repo mute key %q", k)
	}
	other := Item{ID: "ci:x", Repo: "shop", Signal: SigCIRed, Turn: TurnMe}
	if !s.Hidden(other, at(2)) {
		t.Fatal("a repo mute does not hide the repo's items")
	}

	// It is on disk: a new store reads it back.
	s2 := Open(path)
	if !s2.Hidden(c, at(2)) || !s2.Hidden(b, at(60)) {
		t.Fatal("mute/later did not persist")
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("store file: %v %v", fi, err)
	}

	// Done is kept 30 days.
	if err := s2.Done("old", at(0)); err != nil {
		t.Fatal(err)
	}
	s2.Prune(at(0).Add(31 * 24 * time.Hour))
	if _, ok := s2.State().Done["old"]; ok {
		t.Fatal("a 31-day-old done was kept")
	}
	if _, ok := s2.Undo(); ok {
		t.Fatal("prune left an undo for a dropped item")
	}
}

// Spec: setup keeps the answers and makes the rules; SetRules refuses
// more than MaxRules; a broken file reads as the defaults, never a crash.
func TestStoreSetupAndRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "now.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(path)
	if s.State().Setup || len(s.State().Rules) != len(DefaultRules()) {
		t.Fatal("a broken file is not read as the defaults")
	}
	a := Answers{Top: []string{SigReview}, From: []string{"github"}, Show: 10, HideBots: true}
	if err := s.Setup(a); err != nil {
		t.Fatal(err)
	}
	st := Open(path).State()
	if !st.Setup || st.Answers.Show != 10 || !reflect.DeepEqual(st.Answers.From, []string{"github"}) {
		t.Fatalf("setup not kept: %+v", st)
	}
	for _, r := range st.Rules {
		if (r.ID == "reviews") != r.On && r.ID != "nobots" {
			t.Errorf("rule %s on=%v after setup", r.ID, r.On)
		}
	}
	many := DefaultRules()
	for len(many) <= MaxRules {
		many = append(many, Rule{ID: "r" + strings.Repeat("x", len(many)), Name: "R", Sentence: "s", Signal: SigMention, Level: 3})
	}
	if err := s.SetRules(many); err == nil {
		t.Fatal("SetRules took more than MaxRules")
	}
	if err := s.SetRules(DefaultRules()[:2]); err != nil {
		t.Fatal(err)
	}
	if got := len(Open(path).State().Rules); got != 2 {
		t.Fatalf("rules kept %d, want 2", got)
	}
}
