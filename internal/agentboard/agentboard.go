// Package agentboard is the model of the Agent Board (CMP-2): one list of
// all agent work in a window, from the three places it lives (AI runs,
// agent lanes in worktrees, and the AI setup that waits for the user),
// sorted into four columns: NEEDS YOU, WORKING, REVIEW and DONE.
//
// The package only decides: which column a lane or a run is in, why, the
// order, the search and the summary. It owns no data and calls nothing;
// the app builds the cards from what it already has and acts on them.
package agentboard

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Column is where a card is on the board, the most urgent first.
type Column int

const (
	NeedsYou Column = iota // the agent waits for an answer, or something broke
	Working                // an agent works on it now
	Review                 // the work is finished: look at it
	Done                   // merged, closed, or finished a while ago
	ColumnCount
)

// Title is the column's header word.
func (c Column) Title() string {
	switch c {
	case NeedsYou:
		return "NEEDS YOU"
	case Working:
		return "WORKING"
	case Review:
		return "REVIEW"
	}
	return "DONE"
}

// words are what the search matches for the column.
func (c Column) words() string {
	switch c {
	case NeedsYou:
		return "needs you waiting"
	case Working:
		return "working"
	case Review:
		return "review"
	}
	return "done"
}

// Source is where a card comes from.
type Source string

const (
	SourceRun  Source = "run"  // an AI run (the Runs view)
	SourceLane Source = "lane" // a worktree with an agent, a checkpoint or a PR
	SourceAI   Source = "ai"   // AI setup that waits for the user (Itermoc)
)

// Card is one thing on the board.
type Card struct {
	ID     string // what the app opens when the card is picked
	Source Source
	Col    Column
	Title  string    // the task or the lane's name
	Where  string    // the profile and chat, or the branch
	Why    string    // why it is in its column, a few words
	CI     string    // a lane's PR checks: pass, fail, pending or ""
	At     time.Time // when it started or last changed
	Resume bool      // a run that Resume goes on with
}

// ReviewFor is how long a finished run stays in REVIEW before DONE.
const ReviewFor = 24 * time.Hour

// LaneColumn places a worktree lane. agent is the state of the agent in
// it (waiting, working, done or ""), check the checkpoint an agent set
// (working, blocked, done or ""), pr and ci its pull request's state
// (open, merged, closed or "") and checks (pass, fail, pending or "").
// on is false for a lane with nothing to show.
func LaneColumn(agent, check, pr, ci string) (col Column, why string, on bool) {
	closed := pr == "merged" || pr == "closed"
	switch {
	case agent == "waiting":
		return NeedsYou, "waits for you", true
	case ci == "fail" && !closed:
		return NeedsYou, "CI failed", true
	case check == "blocked" && agent != "working":
		return NeedsYou, "blocked", true
	case agent == "working":
		return Working, "working", true
	case pr == "merged":
		return Done, "merged", true
	case pr == "closed":
		return Done, "PR closed", true
	case agent == "done" || check == "done":
		why = "done · review it"
		if w := CIWord(ci); w != "" {
			why += " · " + w
		}
		return Review, why, true
	case pr == "open":
		why = "PR open"
		if w := CIWord(ci); w != "" {
			why += " · " + w
		}
		return Review, why, true
	case check == "working":
		return Working, "working (checkpoint)", true
	}
	return 0, "", false
}

// RunState is where an AI run is.
type RunState int

const (
	RunRunning   RunState = iota // a step works (or it pauses before the next)
	RunStalled                   // a step sends nothing for too long
	RunPaused                    // paused: Resume goes on
	RunResumable                 // stopped with steps left
	RunDone
	RunFailed
	RunCancelled
)

// RunColumn places a run that started age ago.
func RunColumn(st RunState, age time.Duration) Column {
	switch st {
	case RunRunning:
		return Working
	case RunStalled, RunPaused, RunResumable, RunFailed:
		return NeedsYou
	case RunDone:
		if age > ReviewFor {
			return Done
		}
		return Review
	}
	return Done
}

// Sort orders the cards by column, the newest first inside a column; the
// ID breaks ties so the order never jumps between two frames.
func Sort(cards []Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if a.Col != b.Col {
			return a.Col < b.Col
		}
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		return a.ID < b.ID
	})
}

// Match reports whether every word of query is in the card (its title,
// where, why, source, column and CI words), any case.
func Match(c Card, query string) bool {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return true
	}
	hay := strings.ToLower(strings.Join([]string{c.Title, c.Where, c.Why, string(c.Source), c.Col.words(), c.CI, CIWord(c.CI)}, " "))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// Filter keeps the cards that Match query.
func Filter(cards []Card, query string) []Card {
	out := cards[:0:0]
	for _, c := range cards {
		if Match(c, query) {
			out = append(out, c)
		}
	}
	return out
}

// Counts is how many cards each column holds.
func Counts(cards []Card) [ColumnCount]int {
	var n [ColumnCount]int
	for _, c := range cards {
		if c.Col >= 0 && c.Col < ColumnCount {
			n[c.Col]++
		}
	}
	return n
}

// Summary is the board in one line: "2 need you · 1 working · 3 to review".
func Summary(n [ColumnCount]int) string {
	var parts []string
	if v := n[NeedsYou]; v > 0 {
		verb := " need you"
		if v == 1 {
			verb = " needs you"
		}
		parts = append(parts, strconv.Itoa(v)+verb)
	}
	if v := n[Working]; v > 0 {
		parts = append(parts, strconv.Itoa(v)+" working")
	}
	if v := n[Review]; v > 0 {
		parts = append(parts, strconv.Itoa(v)+" to review")
	}
	if v := n[Done]; v > 0 {
		parts = append(parts, strconv.Itoa(v)+" done")
	}
	if len(parts) == 0 {
		return "nothing yet"
	}
	return strings.Join(parts, " · ")
}

// CIWord is a PR's checks in words ("" when there are none).
func CIWord(ci string) string {
	switch ci {
	case "pass":
		return "CI green"
	case "fail":
		return "CI failed"
	case "pending":
		return "CI running"
	}
	return ""
}

// Ago is a short age: "now", "5 min", "3 h", "2 d".
func Ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + " d"
}
