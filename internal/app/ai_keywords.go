package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Keyword detector (OMX-17). "ulw" (or "ultrawork"), "ulw plan" and
// "mass ulw" (or "mulw") anywhere in a message (a whole word, any case,
// not in code) run that one message with a profile. It is never a hidden
// switch: a chip above the input says what will happen while the user
// types, and its × (or Alt+Q) sends the message as plain text.

// aiKeyword is a detected keyword: the word as shown, the profile it
// runs and what that does, for the chip.
type aiKeyword struct {
	word, profile, does string
}

var (
	aiKwUlw  = aiKeyword{"ulw", ai.ProfileUntilDone, "Work until done"}
	aiKwPlan = aiKeyword{"ulw plan", ai.ProfilePlan, "Plan first, then run the steps"}
	aiKwMass = aiKeyword{"mass ulw", ai.ProfilePlan, "Plan, then run steps in parallel"}
)

// aiDetectKeyword finds the strongest keyword of text: mass ulw, then
// ulw plan, then ulw. Code (fenced blocks and `spans`) is skipped; a
// word is split by spaces and may end in punctuation, so "src/ulw" is no
// match.
func aiDetectKeyword(text string) (aiKeyword, bool) {
	words := aiProseWords(text)
	var found *aiKeyword
	for i, w := range words {
		var k *aiKeyword
		switch {
		case w == "mulw", w == "ulw" && i > 0 && words[i-1] == "mass":
			k = &aiKwMass
		case (w == "ulw" || w == "ultrawork") && i+1 < len(words) && words[i+1] == "plan":
			k = &aiKwPlan
		case w == "ulw" || w == "ultrawork":
			k = &aiKwUlw
		}
		if k != nil && (found == nil || aiKwRank(*k) > aiKwRank(*found)) {
			found = k
		}
	}
	if found == nil {
		return aiKeyword{}, false
	}
	return *found, true
}

func aiKwRank(k aiKeyword) int {
	switch k.word {
	case aiKwMass.word:
		return 3
	case aiKwPlan.word:
		return 2
	}
	return 1
}

// aiProseWords are the lower-case words of text outside code, with the
// punctuation around each word cut off.
func aiProseWords(text string) []string {
	var prose strings.Builder
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		parts := strings.Split(line, "`") // odd parts are `code`
		for i := 0; i < len(parts); i += 2 {
			prose.WriteString(parts[i] + " ")
		}
		prose.WriteByte('\n')
	}
	var out []string
	for _, f := range strings.Fields(prose.String()) {
		if w := strings.ToLower(strings.Trim(f, ".,!?;:()[]{}\"'«»")); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// aiInputKeyword is the keyword of the draft the chip shows (false when
// there is none or the user removed it).
func (s *aiState) aiInputKeyword() (aiKeyword, bool) {
	if s == nil || s.kwOff || len(s.input) == 0 {
		return aiKeyword{}, false
	}
	kw, ok := aiDetectKeyword(string(s.input))
	if ok {
		if _, has := s.profiles.Get(kw.profile); !has {
			return aiKeyword{}, false
		}
	}
	return kw, ok
}
