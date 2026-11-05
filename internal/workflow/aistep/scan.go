package aistep

import (
	"slices"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// ChipText is the item chip of a flagged item (plan §6.4).
const ChipText = "⚠ looks like injection"

// The reasons a line is flagged (Chip.Reasons).
const (
	ReasonIgnore  = "asks to ignore the instructions"
	ReasonCommand = "asks to run a command"
	ReasonSecret  = "asks to send a secret"
	ReasonRole    = "tries to change the agent's role"
	ReasonMark    = "fakes a prompt mark"
)

// scanMax caps the text one scan reads (the item cap is 64 KB).
const scanMax = 64 << 10

var scanRules = []struct {
	reason string
	re     *lazyre.Regexp
}{
	{ReasonIgnore, lazyre.New(`(?i)\b(ignore|disregard|forget|override)\b.{0,40}\b(instructions?|prompts?|rules|directions|guidelines)\b`)},
	{ReasonCommand, lazyre.New(`(?i)\b(run|execute|exec)\b.{0,20}\b(this|the following|these|that)\b.{0,15}\b(commands?|scripts?|code|shell)\b`)},
	{ReasonCommand, lazyre.New(`(?i)\b(curl|wget)\b[^\n|]*\|\s*(ba|z)?sh\b`)},
	{ReasonCommand, lazyre.New(`(?i)\brm\s+-[a-z]*r[a-z]*f?\s+/`)},
	{ReasonSecret, lazyre.New(`(?i)\b(send|post|upload|email|forward|leak|exfiltrate|paste|reveal)\b.{0,40}(\b(tokens?|passwords?|secrets?|api[ _-]?keys?|credentials?|private keys?|ssh keys?)\b|\.env\b)`)},
	{ReasonRole, lazyre.New(`(?i)\b(you are now|from now on,? you|act as (an?|the)\b|new instructions|system prompt|developer mode|jailbreak)`)},
	{ReasonMark, lazyre.New(`(?i)<<<\s*(end\s+)?item\b|<\|im_(start|end)\|>|</?(system|assistant|instructions)>`)},
}

// Chip is the scan result an item's chip shows. The zero Chip is a
// clean item.
type Chip struct {
	// Text is ChipText when the item is flagged, else "".
	Text string
	// Reasons are the reasons found, each once, in rule order of first
	// sight.
	Reasons []string
	// Lines are the flagged lines (1-based).
	Lines []int
}

// Flagged reports whether the item looks like an injection.
func (c Chip) Flagged() bool { return len(c.Reasons) > 0 }

// Scan reads item text for lines that look like orders to the agent
// ("ignore previous", "run this command", "send the token"). It only
// gives the chip's data: the item still goes to the model as a marked
// data block, and what a flag stops (an agent step's item waits for the
// user's card) is the engine's.
func Scan(text string) Chip {
	if len(text) > scanMax {
		text = text[:scanMax]
	}
	var c Chip
	for i, line := range strings.Split(text, "\n") {
		hit := false
		for _, r := range scanRules {
			if !r.re.MatchString(line) {
				continue
			}
			hit = true
			if !slices.Contains(c.Reasons, r.reason) {
				c.Reasons = append(c.Reasons, r.reason)
			}
		}
		if hit {
			c.Lines = append(c.Lines, i+1)
		}
	}
	if c.Flagged() {
		c.Text = ChipText
	}
	return c
}
