package agentrun

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// ── Team mode (OMX-40) ────────────────────────────────────────────────
//
// With Options.Team on and more than one step at a time, every agent
// step of a run gets four tools: team_send (a note to another running
// step, through the same mailbox as Send), team_claim / team_release (a
// shared list of claimed keys, mostly file paths, so two steps do not
// work on the same thing) and team_claims (read the list). A step's
// claims are dropped when it ends. Nothing here touches the user's
// files, so the tools ask no card; the app's TeamCheck refuses secrets.

// TeamMaxClaims caps the claim list of one run; TeamKeyMax a key (runes).
const (
	TeamMaxClaims = 200
	TeamKeyMax    = 300
)

// TeamNote is added to a team step's system prompt.
const TeamNote = "\n\nTeam mode: other steps of this run work at the same time. Before you change a file or take a " +
	"piece of work, call team_claim with its path or a short name; when it is held by another step, pick other work " +
	"or team_send that step a note. team_send(to, message) reaches one running step (id or title) or all of them " +
	"(to: all); notes from others arrive as messages while you work. Your claims end with your step; team_release " +
	"one you no longer need. Keep notes short and concrete."

// team is a run's claim list (key → node id).
type team struct {
	mu     sync.Mutex
	claims map[string]string
}

// teamOn reports whether team tools go to the steps of this run.
func (h *Handle) teamOn() bool { return h.o.Team && h.o.Parallel > 1 }

// running is the ids of the steps whose mailbox is open now, sorted.
func (b *mailbox) running() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(b.open))
	for id := range b.open {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// stepTitle is the title of node id ("" when it has none).
func (h *Handle) stepTitle(id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.run.Nodes {
		if n.ID == id {
			return strings.TrimSpace(n.Title)
		}
	}
	return ""
}

// stepKnown reports whether the run has a node named to (by id or title)
// and returns its id.
func (h *Handle) stepKnown(to string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.run.Nodes {
		if n.ID == to || strings.EqualFold(strings.TrimSpace(n.Title), to) {
			return n.ID, true
		}
	}
	return "", false
}

// TeamSend queues text from step from for step to (id or title), or for
// every other running step when to is "" or "all". It returns the ids
// the note went to.
func (h *Handle) TeamSend(from, to, text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("the message is empty")
	}
	if c := h.o.TeamCheck; c != nil {
		if err := c(text); err != nil {
			return nil, err
		}
	}
	head := "From step " + from
	if t := h.stepTitle(from); t != "" {
		head += " (" + t + ")"
	}
	body := head + ": " + text
	to = strings.TrimSpace(to)
	var targets []string
	if to == "" || strings.EqualFold(to, "all") {
		for _, id := range h.mail.running() {
			if id != from {
				targets = append(targets, id)
			}
		}
		if len(targets) == 0 {
			return nil, errors.New("no other step runs now")
		}
	} else {
		id, ok := h.stepKnown(to)
		switch {
		case !ok:
			return nil, fmt.Errorf("no step %q in this run (running: %s)", to, strings.Join(h.mail.running(), ", "))
		case id == from:
			return nil, errors.New("this is yourself: send to another step")
		}
		targets = []string{id}
	}
	var sent []string
	for _, id := range targets {
		if err := h.Send(id, body); err != nil {
			if len(targets) == 1 {
				if err.Error() == "the step does not run now" {
					return nil, fmt.Errorf("step %s does not run now", id)
				}
				return nil, err
			}
			continue // it ended meanwhile
		}
		sent = append(sent, id)
	}
	if len(sent) == 0 {
		return nil, errors.New("no other step runs now")
	}
	return sent, nil
}

// Claim gives key to node unless another step holds it: then ok is
// false and owner names that step.
func (h *Handle) Claim(node, key string) (owner string, ok bool, err error) {
	key = strings.TrimSpace(key)
	switch {
	case key == "":
		return "", false, errors.New("the key is empty")
	case len([]rune(key)) > TeamKeyMax:
		return "", false, fmt.Errorf("the key is too long (at most %d characters)", TeamKeyMax)
	}
	t := &h.team
	t.mu.Lock()
	defer t.mu.Unlock()
	if o, held := t.claims[key]; held {
		return o, o == node, nil
	}
	if len(t.claims) >= TeamMaxClaims {
		return "", false, fmt.Errorf("the run holds %d claims already: release some first", TeamMaxClaims)
	}
	if t.claims == nil {
		t.claims = map[string]string{}
	}
	t.claims[key] = node
	return node, true, nil
}

// Release drops node's claim on key.
func (h *Handle) Release(node, key string) error {
	key = strings.TrimSpace(key)
	t := &h.team
	t.mu.Lock()
	defer t.mu.Unlock()
	switch o, held := t.claims[key]; {
	case !held:
		return fmt.Errorf("nobody holds %q", key)
	case o != node:
		return fmt.Errorf("%q is held by step %s, not by you", key, o)
	}
	delete(t.claims, key)
	return nil
}

// Claims is a copy of the claim list (key → step id).
func (h *Handle) Claims() map[string]string {
	t := &h.team
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]string, len(t.claims))
	for k, v := range t.claims {
		out[k] = v
	}
	return out
}

// releaseAll drops every claim of node (it ended) and returns the keys,
// sorted.
func (h *Handle) releaseAll(node string) []string {
	t := &h.team
	t.mu.Lock()
	defer t.mu.Unlock()
	var keys []string
	for k, v := range t.claims {
		if v == node {
			keys = append(keys, k)
			delete(t.claims, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// withTeam adds the team tools and TeamNote to the agent of step n when
// team mode is on; it returns the history to run.
func (h *Handle) withTeam(n agentdag.Node, agent *ai.Agent, history []ai.Message) []ai.Message {
	if !h.teamOn() || agent == nil {
		return history
	}
	agent.Tools = append(agent.Tools, h.teamTools(n.ID)...)
	if len(history) > 0 && history[0].Role == ai.RoleSystem {
		history = slices.Clone(history)
		history[0].Content += TeamNote
	}
	return history
}

// teamTools are the four team tools of step node.
func (h *Handle) teamTools(node string) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	arg := func(a map[string]any, k string) string { return strings.TrimSpace(ai.ArgString(a, k)) }
	return []ai.Tool{
		{
			Name:    "team_send",
			Display: "note to another step",
			Description: "Team mode: send a short note to another step of this run that runs now (to: its id or title), " +
				"or to every other running step (to: all). It reads it after its current tool call.",
			Params: map[string]any{
				"to":      str("step id or title, or all (default)"),
				"message": str("the note, short and concrete"),
			},
			Required: []string{"message"},
			Describe: func(a map[string]any) string { return "Note to " + orAll(arg(a, "to")) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				to, err := h.TeamSend(node, arg(a, "to"), ai.ArgString(a, "message"))
				if err != nil {
					return "", err
				}
				return "Sent to " + strings.Join(to, ", ") + ".", nil
			},
		},
		{
			Name:        "team_claim",
			Display:     "claim work",
			ReadOnly:    true,
			Description: "Team mode: claim a key (a file path or a short name of a piece of work) for this step before you work on it.",
			Params:      map[string]any{"key": str("file path or short work name")},
			Required:    []string{"key"},
			Describe:    func(a map[string]any) string { return "Claim " + arg(a, "key") },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				key := arg(a, "key")
				owner, ok, err := h.Claim(node, key)
				switch {
				case err != nil:
					return "", err
				case !ok:
					return fmt.Sprintf("%q is held by step %s: pick other work or team_send it a note.", key, owner), nil
				}
				return fmt.Sprintf("You claimed %q.", key), nil
			},
		},
		{
			Name:        "team_release",
			Display:     "release a claim",
			ReadOnly:    true,
			Description: "Team mode: release a key this step claimed (claims also end with the step).",
			Params:      map[string]any{"key": str("the claimed key")},
			Required:    []string{"key"},
			Describe:    func(a map[string]any) string { return "Release " + arg(a, "key") },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				if err := h.Release(node, arg(a, "key")); err != nil {
					return "", err
				}
				return "Released.", nil
			},
		},
		{
			Name:        "team_claims",
			Display:     "list claims",
			ReadOnly:    true,
			Description: "Team mode: list the claimed keys of this run and the step that holds each, and the steps that run now.",
			Params:      map[string]any{},
			Describe:    func(map[string]any) string { return "List claims" },
			Run: func(context.Context, map[string]any) (string, error) {
				c := h.Claims()
				keys := make([]string, 0, len(c))
				for k := range c {
					keys = append(keys, k)
				}
				slices.Sort(keys)
				var b strings.Builder
				fmt.Fprintf(&b, "You are step %s. Running: %s.\n", node, strings.Join(h.mail.running(), ", "))
				if len(keys) == 0 {
					b.WriteString("No claims.")
				}
				for _, k := range keys {
					fmt.Fprintf(&b, "%s: %s\n", k, c[k])
				}
				return strings.TrimSpace(b.String()), nil
			},
		},
	}
}

func orAll(s string) string {
	if s == "" {
		return "all"
	}
	return s
}
