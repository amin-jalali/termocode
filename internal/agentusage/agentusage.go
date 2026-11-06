// Package agentusage keeps what termocode knows about the plan limits of
// the outside agents (Claude Code, Codex) per account: the share of the
// plan window used, when it resets, the last limit hit and the tokens of
// today's sessions (AGX-12). The numbers come only from (1) the limit
// message on an agent tab's screen and (2) the agents' own session files
// when the user turned agent_transcripts on. No vendor endpoint is
// called (owner decision D4). Kept in memory; safe for concurrent use.
package agentusage

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Key is one agent account: the agent id ("claude", "codex") and the
// account label ("" = the CLI's own default folder).
type Key struct {
	CLI, Account string
}

// Window is one account's plan window.
type Window struct {
	Key
	// UsedPct is the used share (0–100) when pctAt is set and the
	// window has not reset since.
	UsedPct float64
	pctAt   time.Time
	// ResetAt is when the window resets (zero: not known).
	ResetAt time.Time
	// LastLimit is when a limit was last seen; limitReset its reset.
	LastLimit  time.Time
	limitReset time.Time
	// TokensToday is the in + out tokens of today's sessions.
	TokensToday int
	day         string
	sessions    map[string]int
}

// PctKnown says whether UsedPct holds a share.
func (w Window) PctKnown() bool { return !w.pctAt.IsZero() }

// Limited says whether a limit is on at now: seen, and its reset (when
// known) has not come; a limit without a reset time counts for 5 hours.
func (w Window) Limited(now time.Time) bool {
	if w.LastLimit.IsZero() {
		return false
	}
	if w.limitReset.IsZero() {
		return now.Sub(w.LastLimit) < 5*time.Hour
	}
	return now.Before(w.limitReset)
}

// Book is the windows of every account.
type Book struct {
	mu sync.Mutex
	w  map[Key]*Window
}

// New is an empty book.
func New() *Book { return &Book{w: map[Key]*Window{}} }

func (b *Book) get(k Key) *Window {
	w := b.w[k]
	if w == nil {
		w = &Window{Key: k}
		b.w[k] = w
	}
	return w
}

// Limit records a limit message seen at at, resetting at reset (zero:
// not said). msg is not kept: only the times count.
func (b *Book) Limit(k Key, _ string, reset, at time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w := b.get(k)
	w.LastLimit, w.limitReset = at, reset
	if !reset.IsZero() {
		w.ResetAt = reset
	}
}

// Transcript records a session's numbers read at at: its plan share
// (pct ≤ 0: not recorded) and reset, and its token total so far.
func (b *Book) Transcript(k Key, session string, pct float64, reset time.Time, tokens int, at time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w := b.get(k)
	if pct > 0 && !at.Before(w.pctAt) {
		w.UsedPct, w.pctAt = min(pct, 100), at
		if !reset.IsZero() {
			w.ResetAt = reset
		}
	}
	day := at.Format("2006-01-02")
	if day < w.day {
		return // an older day's session
	}
	if day > w.day {
		w.day, w.sessions = day, map[string]int{}
	}
	w.sessions[session] = tokens
	w.TokensToday = 0
	for _, n := range w.sessions {
		w.TokensToday += n
	}
}

// Windows lists the accounts, Claude first, then Codex, then by label
// (the default account first). A share whose window reset is over at
// now is dropped.
func (b *Book) Windows(now time.Time) []Window {
	b.mu.Lock()
	out := make([]Window, 0, len(b.w))
	for _, w := range b.w {
		c := *w
		c.sessions = nil
		if c.PctKnown() && !c.ResetAt.IsZero() && !now.Before(c.ResetAt) && c.pctAt.Before(c.ResetAt) {
			c.UsedPct, c.pctAt = 0, time.Time{}
		}
		out = append(out, c)
	}
	b.mu.Unlock()
	rank := func(cli string) int {
		switch cli {
		case "claude":
			return 0
		case "codex":
			return 1
		}
		return 2
	}
	sort.Slice(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if rank(a.CLI) != rank(c.CLI) {
			return rank(a.CLI) < rank(c.CLI)
		}
		if a.CLI != c.CLI {
			return a.CLI < c.CLI
		}
		return a.Account < c.Account
	})
	return out
}

// Name is the short agent name of the pill: "Claude", "Codex".
func Name(cli string) string {
	switch cli {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	}
	if cli == "" {
		return ""
	}
	return strings.ToUpper(cli[:1]) + cli[1:]
}

// Label is "Claude" or "Claude work".
func (w Window) Label() string {
	if w.Account == "" {
		return Name(w.CLI)
	}
	return Name(w.CLI) + " " + w.Account
}

// Pct is "62%".
func Pct(v float64) string { return strconv.Itoa(int(v+0.5)) + "%" }

// pillMax is how many accounts the pill names.
const pillMax = 2

// Pill is the status-bar pill: "✦ Claude 62% ↻14:30 · Codex limit
// ↻15:00", and its narrow form "✦62%" (the first account only). Both ""
// when nothing is known. Times are in now's location.
func Pill(ws []Window, now time.Time) (long, short string) {
	var parts []string
	for _, w := range ws {
		if len(parts) == pillMax {
			break
		}
		var what string
		switch {
		case w.Limited(now):
			what = "limit"
		case w.PctKnown():
			what = Pct(w.UsedPct)
		default:
			continue
		}
		if short == "" {
			short = "✦" + what
		}
		p := w.Label() + " " + what
		if !w.ResetAt.IsZero() && now.Before(w.ResetAt) {
			p += " ↻" + w.ResetAt.In(now.Location()).Format("15:04")
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "", ""
	}
	return "✦ " + strings.Join(parts, " · "), short
}
