package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// ── Auto model pick (CMP-25) ──────────────────────────────────────────
//
// ai_router picks the model slot of each plain chat turn: a rule whose
// words the message holds wins (first match), else a local guess of how
// hard the turn is sends simple turns to the quick slot and hard ones to
// the deep slot. It runs on this machine, with no extra request, and
// the chat shows the pick and why on a line for the user only. A profile
// that names its own slot, an agent run and a turn with a slot that can
// not run keep their model.

// Router modes (Router.Mode).
const (
	RouterOff  = "off"
	RouterAuto = "auto"
)

// Router limits.
const (
	MaxRouteRules = 20
	MaxRouteWords = 20
	// RouteQuickMaxRunes is the longest message that can count as a
	// quick turn.
	RouteQuickMaxRunes = 240
	// RouteHardRunes is the length from which a message counts as hard.
	RouteHardRunes = 800
	// RouteHardLines is the line count from which a message is hard.
	RouteHardLines = 8
)

// Router is config.json "ai_router".
type Router struct {
	// Mode is auto (pick per turn) or off ("" too: the default).
	Mode string `json:"mode,omitempty"`
	// Quick and Deep are the slots of simple and hard turns ("": quick
	// and main).
	Quick string `json:"quick,omitempty"`
	Deep  string `json:"deep,omitempty"`
	// Rules come first, in order: the first whose words match wins.
	Rules []RouteRule `json:"rules,omitempty"`
}

// RouteRule sends a turn whose message holds one of Words (whole words
// or phrases, any case) to Slot.
type RouteRule struct {
	Words []string `json:"words"`
	Slot  string   `json:"slot"`
}

// RouteInput is what the router looks at: the message, the files and
// images attached to it and the turn's think keyword (OMX-5).
type RouteInput struct {
	Text        string
	Attachments int
	Images      int
	Think       string
}

// RouteChoice is the slot of a turn and the short reason shown in the
// chat. Rule is the 1-based rule that matched (0: the guess).
type RouteChoice struct {
	Slot   string
	Reason string
	Hard   bool
	Rule   int
}

// On reports whether the router picks models.
func (r Router) On() bool { return strings.EqualFold(strings.TrimSpace(r.Mode), RouterAuto) }

// QuickSlot is the slot of simple turns.
func (r Router) QuickSlot() string {
	if s := strings.TrimSpace(r.Quick); s != "" {
		return s
	}
	return SlotQuick
}

// DeepSlot is the slot of hard turns.
func (r Router) DeepSlot() string {
	if s := strings.TrimSpace(r.Deep); s != "" {
		return s
	}
	return SlotMain
}

// CheckRouter reports why ai_router can not be used (nil = fine).
func CheckRouter(r Router) error {
	switch strings.ToLower(strings.TrimSpace(r.Mode)) {
	case "", RouterOff, RouterAuto:
	default:
		return fmt.Errorf("mode must be %s or %s", RouterAuto, RouterOff)
	}
	slotOK := func(s string) bool {
		s = strings.TrimSpace(s)
		if _, _, ok := SplitSyntheticSlot(s); ok {
			return true
		}
		return profileIDRe.MatchString(s)
	}
	for _, s := range []string{r.Quick, r.Deep} {
		if strings.TrimSpace(s) != "" && !slotOK(s) {
			return fmt.Errorf("%q is not a slot name or <provider>/<model>", s)
		}
	}
	if len(r.Rules) > MaxRouteRules {
		return fmt.Errorf("at most %d rules", MaxRouteRules)
	}
	for i, ru := range r.Rules {
		if !slotOK(ru.Slot) {
			return fmt.Errorf("rule %d: slot %q is not a slot name or <provider>/<model>", i+1, ru.Slot)
		}
		n := 0
		for _, w := range ru.Words {
			if strings.TrimSpace(w) != "" {
				n++
			}
		}
		if n == 0 {
			return fmt.Errorf("rule %d: words is empty", i+1)
		}
		if len(ru.Words) > MaxRouteWords {
			return fmt.Errorf("rule %d: at most %d words", i+1, MaxRouteWords)
		}
	}
	return nil
}

// routeHardWords make a turn hard: the user asks for design, deep
// debugging or a big change.
var routeHardWords = []string{
	"refactor", "architecture", "architect", "design", "debug", "race", "deadlock", "leak",
	"migrate", "migration", "implement", "optimize", "optimise", "performance", "security",
	"rewrite", "review", "plan", "why", "investigate", "root cause", "step by step", "prove",
	"concurrency", "algorithm", "think", "ultrathink",
}

// Pick returns the slot of a turn ("" when the router is off or broken).
func (r Router) Pick(in RouteInput) RouteChoice {
	if !r.On() || CheckRouter(r) != nil {
		return RouteChoice{}
	}
	low := strings.ToLower(in.Text)
	for i, ru := range r.Rules {
		for _, w := range ru.Words {
			if w = strings.TrimSpace(w); w != "" && hasWord(low, strings.ToLower(w)) {
				return RouteChoice{Slot: strings.TrimSpace(ru.Slot), Reason: fmt.Sprintf("rule %d: %q", i+1, w), Rule: i + 1}
			}
		}
	}
	hard, reason := RouteHard(in)
	if hard {
		return RouteChoice{Slot: r.DeepSlot(), Reason: reason, Hard: true}
	}
	return RouteChoice{Slot: r.QuickSlot(), Reason: reason}
}

// RouteHard is the local guess of how hard a turn is, with its reason.
// When in doubt the turn is hard: the deep model never answers worse.
func RouteHard(in RouteInput) (bool, string) {
	text := strings.TrimSpace(in.Text)
	runes := len([]rune(text))
	switch {
	case in.Think != "":
		return true, "think was asked"
	case in.Images > 0:
		return true, "an image is attached"
	case in.Attachments > 0:
		return true, "a file is attached"
	case strings.Contains(text, "```"):
		return true, "the message holds code"
	case runes >= RouteHardRunes:
		return true, "a long message"
	case strings.Count(text, "\n")+1 >= RouteHardLines:
		return true, "a long message"
	}
	low := strings.ToLower(text)
	for _, w := range routeHardWords {
		if hasWord(low, w) {
			return true, fmt.Sprintf("it asks to %s", w)
		}
	}
	if runes <= RouteQuickMaxRunes {
		return false, "a short, simple message"
	}
	return true, "a longer message"
}

// hasWord reports whether low holds w as whole words (both lower-case).
func hasWord(low, w string) bool {
	if w == "" {
		return false
	}
	from := 0
	for {
		i := strings.Index(low[from:], w)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(w)
		before := i == 0 || !wordRune(lastRune(low[:i]))
		after := end == len(low) || !wordRune(firstRune(low[end:]))
		if before && after {
			return true
		}
		from = i + 1
	}
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

// SaveRouterMode writes ai_router.mode (auto or off) in config.json at
// path; the router's other fields and every other key stay.
func SaveRouterMode(path, mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != RouterAuto && mode != RouterOff {
		return errors.New("ai_router mode must be auto or off")
	}
	return cfgfile.UpdateRaw(path, func(cfg map[string]json.RawMessage) error {
		r, err := rawObject(cfg["ai_router"], "ai_router")
		if err != nil {
			return err
		}
		b, _ := json.Marshal(mode)
		r["mode"] = b
		rb, err := json.Marshal(r)
		if err != nil {
			return err
		}
		cfg["ai_router"] = rb
		return nil
	})
}
