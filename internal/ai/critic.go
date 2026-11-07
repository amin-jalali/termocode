package ai

import (
	"strings"
)

// CMP-53: the critic. When a lane (a step's own git worktree) ends, a
// second model reads the lane's diff and says whether it is ready to
// merge. It only reads: it gets the diff as text and no tools. Its
// verdict waits on the step's card; Merge with open problems asks first.

// ai_critic values: "" (the default, "auto") reviews every finished
// lane; "off" never does.
const CriticOff = "off"

// States of a lane's critic (StoredCritic.State).
const (
	CriticRunning = "running" // the critic model reads the diff
	CriticOK      = "ok"      // no problems found
	CriticIssues  = "issues"  // problems found (Notes lists them)
	CriticFailed  = "failed"  // no verdict: no model, an error, no answer
)

// CriticMaxNotes caps the notes kept on the card (bytes).
const CriticMaxNotes = 4000

// CriticMaxDiff caps the diff sent to the critic (bytes).
const CriticMaxDiff = 48 << 10

// UseCritic reports whether finished lanes get a critic review.
func (s Settings) UseCritic() bool { return s.Critic != CriticOff }

// StoredCritic is the critic's review of one lane.
type StoredCritic struct {
	State string `json:"state"`
	Model string `json:"model,omitempty"`
	Notes string `json:"notes,omitempty"`
	// Round counts the reviews of this lane; an answer of an older round
	// (the user asked again) is dropped.
	Round int `json:"round,omitempty"`
}

// Done reports whether the critic has a verdict (or failed).
func (c *StoredCritic) Done() bool { return c != nil && c.State != CriticRunning }

// CriticSlot is the model slot of the critic of a step that ran on
// stepSlot: set (ai_critic_slot) when the user named one, else the other
// built-in model: quick for a step on main, main for every other.
func CriticSlot(stepSlot, set string) string {
	if set = strings.TrimSpace(set); set != "" {
		return set
	}
	if stepSlot == "" || stepSlot == SlotMain {
		return SlotQuick
	}
	return SlotMain
}

// criticSystem is the critic's system prompt.
const criticSystem = "You are a strict code reviewer, a second pair of eyes. Another AI agent made the changes below for a task. " +
	"Check them before the user merges them: bugs, broken or missing parts of the task, risky changes, missing tests, secrets in code. " +
	"Do not rewrite the code. Start your answer with exactly one line: \"VERDICT: OK\" when it is ready to merge, " +
	"or \"VERDICT: ISSUES\" when it is not. After an ISSUES line list each problem on its own line as \"- file:line what is wrong\", most important first."

// CriticPrompt is the critic's request: the task and the lane's diff
// (cut: the diff was longer). Registered secrets are hidden.
func CriticPrompt(task, diff string, cut bool) []Message {
	var b strings.Builder
	if task = strings.TrimSpace(task); task != "" {
		b.WriteString("The agent's task:\n" + task + "\n\n")
	}
	b.WriteString("Its changes:\n```diff\n" + strings.TrimRight(RedactSecrets(diff), "\n") + "\n```\n")
	if cut {
		b.WriteString("[cut here: the rest of the diff is longer]\n")
	}
	return []Message{{Role: RoleSystem, Content: criticSystem}, {Role: RoleUser, Content: b.String()}}
}

// ParseCritic reads the critic's answer: its state and notes. An answer
// without a verdict line counts as problems (the user reads it).
func ParseCritic(text string) (state, notes string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return CriticFailed, "The critic gave no answer"
	}
	first, rest, _ := strings.Cut(text, "\n")
	head := strings.TrimSpace(first)
	if v, ok := cutFoldPrefix(head, "verdict:"); ok {
		v = strings.TrimSpace(v)
		word, tail := v, ""
		if i := strings.IndexFunc(v, func(r rune) bool { return r == ' ' || r == '\t' }); i >= 0 {
			word, tail = v[:i], strings.TrimSpace(v[i:])
		}
		tail = strings.TrimSpace(strings.TrimLeft(tail, "—–-:"))
		notes = strings.TrimSpace(strings.TrimSpace(tail + "\n" + rest))
		switch strings.ToLower(strings.TrimRight(word, ".,;")) {
		case "ok", "pass", "approve", "approved":
			return CriticOK, clipCritic(notes)
		case "issues", "issue", "fail", "problems":
			return CriticIssues, clipCritic(notes)
		}
	}
	return CriticIssues, clipCritic("The critic gave no clear verdict:\n" + text)
}

// cutFoldPrefix is strings.CutPrefix ignoring ASCII case.
func cutFoldPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

func clipCritic(s string) string {
	if len(s) <= CriticMaxNotes {
		return s
	}
	cut := CriticMaxNotes
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// utf8Start reports whether b starts a UTF-8 sequence.
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
