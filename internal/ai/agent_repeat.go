package ai

import (
	"encoding/json"
	"fmt"
)

// DefaultRepeatLimit is how many times in a row the agent lets the model
// make the same tool call (same tool, same arguments) before it stops
// that call (config.json "ai_repeat_call_limit").
const DefaultRepeatLimit = 4

// repeatGuard watches the tool calls of one turn for a loop: the same
// call again and again. The limit-th call in a row is not run (the model
// gets a note); the same call once more after the note ends the turn.
type repeatGuard struct {
	limit  int // <= 0: off
	last   string
	same   int
	warned bool // the model got the note for this call
}

func newRepeatGuard(limit int) repeatGuard {
	if limit == 0 {
		limit = DefaultRepeatLimit
	}
	return repeatGuard{limit: limit}
}

// check marks the calls not to run and reports whether the turn ends.
func (g *repeatGuard) check(calls []ToolCall) (blocked []bool, stop bool) {
	blocked = make([]bool, len(calls))
	if g.limit <= 0 {
		return blocked, false
	}
	for i, c := range calls {
		sig := callSignature(c)
		if sig == g.last {
			g.same++
		} else {
			g.last, g.same, g.warned = sig, 1, false
		}
		if g.same >= g.limit {
			blocked[i] = true
			if g.warned {
				stop = true
			}
			g.warned = true
		}
	}
	return blocked, stop
}

// callSignature is a call's tool and arguments as one string (JSON
// sorts the keys of a map, so equal arguments give equal strings).
func callSignature(c ToolCall) string {
	b, err := json.Marshal(c.Args)
	if err != nil {
		b = []byte(fmt.Sprint(c.Args))
	}
	return c.Name + "\x00" + string(b)
}

// RepeatCallNote is what the model gets instead of the result of a call
// it made n times in a row.
func RepeatCallNote(name string, n int) string {
	return fmt.Sprintf("%s: not run. You made this same call (same arguments) %d times in a row; the result will not change. "+
		"Use what you already have, try another way, or tell the user what blocks you.", name, n)
}

// RepeatStopText is the note shown when the model made the same call
// again after RepeatCallNote: the turn ends.
func RepeatStopText(name string) string {
	return fmt.Sprintf("\n\n**Stopped: the AI called %s with the same arguments again and again.** "+
		"It was told to try another way and did not. Tell it what to do next.", name)
}

// repeatBlocked tells the chat about a call the guard did not run and
// returns the model's note.
func (a *Agent) repeatBlocked(call ToolCall, n int, emit func(Event)) string {
	label := call.Name
	if t, ok := FindTool(a.Tools, call.Name); ok {
		label = t.Label()
	}
	note := RepeatCallNote(call.Name, n)
	emit(Event{Kind: EventToolStart, Tool: call.Name, Label: label, Detail: call.Name, Args: call.Args})
	emit(Event{Kind: EventToolNote, Tool: call.Name, Label: label, Detail: fmt.Sprintf("Same call %d times in a row: not run", n)})
	emit(Event{Kind: EventToolResult, Tool: call.Name, Label: label, Detail: note, Gate: GateInvalid, Decision: "repeat stopped"})
	return note
}
