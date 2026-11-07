// Package agentgoal is the "loop until verify passes" of an outside
// agent tab (CMP-19). The user names a verify command; each time the
// agent's turn ends termocode runs it. Exit 0 ends the loop. Any other
// exit types the failure into the agent and asks it to fix it, until a
// cap is hit: the rounds, the wall time, or the agent's tokens.
//
// The package only decides: it never runs a command or types. The app
// (internal/app/agent_goal.go) does both behind its guards.
package agentgoal

import (
	"fmt"
	"strings"
	"time"
)

// Round choices offered on the start card. Default is the primary one.
var RoundChoices = []int{3, 5, 10}

const (
	DefaultRounds = 5
	// PerRound is the wall time each round adds to the time cap.
	PerRound = 20 * time.Minute
	// TailLines is how many last lines of the verify output are typed.
	TailLines = 30
	// TailBytes caps the typed failure text.
	TailBytes = 1500
)

// Limits caps one loop.
type Limits struct {
	Rounds int           // retries typed at most (≥ 1)
	Time   time.Duration // wall time from the start (0: Rounds × PerRound)
	Tokens int           // agent tokens from the start (0: no cap)
}

// Goal is one running loop of one agent tab.
type Goal struct {
	Verify string // the shell line
	Dir    string // where it runs
	Limits
	Round       int // retries typed so far
	Started     time.Time
	StartTokens int  // the agent's tokens today at the start
	Verifying   bool // a verify job runs now
}

// New starts a goal at now. rounds < 1 is DefaultRounds.
func New(verify, dir string, rounds, maxTokens, startTokens int, now time.Time) *Goal {
	if rounds < 1 {
		rounds = DefaultRounds
	}
	return &Goal{
		Verify:      strings.TrimSpace(verify),
		Dir:         dir,
		Limits:      Limits{Rounds: rounds, Time: time.Duration(rounds) * PerRound, Tokens: max(maxTokens, 0)},
		Started:     now,
		StartTokens: startTokens,
	}
}

// Outcome is what the loop does after a verify exit.
type Outcome int

const (
	Met        Outcome = iota // exit 0: the loop ends, goal met
	Retry                     // type the failure, one more round
	StopRounds                // the last round failed
	StopTime                  // the time cap is over
	StopTokens                // the token cap is over
)

// Stopped reports an outcome that ends the loop without success.
func (o Outcome) Stopped() bool { return o >= StopRounds }

// Reason is the short text of a stop ("" for Met and Retry).
func (o Outcome) Reason() string {
	switch o {
	case StopRounds:
		return "no rounds left"
	case StopTime:
		return "the time cap is over"
	case StopTokens:
		return "the token cap is over"
	}
	return ""
}

// TokensUsed is the agent tokens since the start. tokensNow < start (a
// new day) counts tokensNow alone.
func (g *Goal) TokensUsed(tokensNow int) int {
	if tokensNow < g.StartTokens {
		return tokensNow
	}
	return tokensNow - g.StartTokens
}

// Next decides after a verify exit code at now. tokensNow < 0: the
// agent's tokens are not known, so the token cap cannot stop the loop.
func (g *Goal) Next(code int, now time.Time, tokensNow int) Outcome {
	g.Verifying = false
	if code == 0 {
		return Met
	}
	if g.Tokens > 0 && tokensNow >= 0 && g.TokensUsed(tokensNow) >= g.Tokens {
		return StopTokens
	}
	if g.Time > 0 && !now.Before(g.Started.Add(g.Time)) {
		return StopTime
	}
	if g.Round >= g.Rounds {
		return StopRounds
	}
	return Retry
}

// Label is the tab bar mark: "goal 2/5".
func (g *Goal) Label() string {
	return fmt.Sprintf("goal %d/%d", g.Round, g.Rounds)
}

// Prompt is the one-line text typed into the agent for round round.
// tail is the end of the verify output (already redacted): its lines
// are joined with " | " so that no newline sends the text early.
func Prompt(verify string, code, round, rounds int, tail string) string {
	lines := Tail(tail, TailLines)
	out := strings.Join(lines, " | ")
	if len(out) > TailBytes {
		out = "… " + cutStart(out, TailBytes)
	}
	s := fmt.Sprintf("The check `%s` failed (exit %d, round %d of %d). Fix the cause, then end your turn; termocode runs the check again.", verify, code, round, rounds)
	if out != "" {
		s += " Last output: " + out
	}
	return s
}

// Tail is the last n non-blank lines of s, trimmed, without control
// characters.
func Tail(s string, n int) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}, l))
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// cutStart keeps the last n bytes of s on a rune start.
func cutStart(s string, n int) string {
	i := len(s) - n
	for i < len(s) && i > 0 && s[i]&0xC0 == 0x80 {
		i++
	}
	return s[i:]
}
