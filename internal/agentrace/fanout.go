package agentrace

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Count is one agent and how many lanes run it (CMP-18).
type Count struct {
	Agent string
	N     int
}

// Counts are the agents of ids with how often each comes, in first-seen
// order.
func Counts(ids []string) []Count {
	var out []Count
	at := map[string]int{}
	for _, id := range ids {
		if i, ok := at[id]; ok {
			out[i].N++
			continue
		}
		at[id] = len(out)
		out = append(out, Count{Agent: id, N: 1})
	}
	return out
}

// FanoutText is ids as a fan-out count: "claude:3,codex,aider" (a count of
// one is the bare id).
func FanoutText(ids []string) string {
	var parts []string
	for _, c := range Counts(ids) {
		if c.N == 1 {
			parts = append(parts, c.Agent)
		} else {
			parts = append(parts, c.Agent+":"+strconv.Itoa(c.N))
		}
	}
	return strings.Join(parts, ",")
}

// ParseFanout reads a fan-out count such as "claude:3,codex:2" (commas or
// spaces between items; a bare id is one lane) into one agent id per
// lane, in order. Each count is 1 to MaxLanes and the total at most
// MaxLanes; whether there are enough lanes is New's check.
func ParseFanout(spec string) ([]string, error) {
	items := strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(items) == 0 {
		return nil, errors.New("the fan-out lists no agents (e.g. claude:3,codex:2)")
	}
	var out []string
	for _, it := range items {
		id, num, has := strings.Cut(it, ":")
		n := 1
		if has {
			v, err := strconv.Atoi(num)
			if err != nil || v < 1 || v > MaxLanes {
				return nil, fmt.Errorf("%q: the count must be 1 to %d", it, MaxLanes)
			}
			n = v
		}
		if !word(id) {
			return nil, fmt.Errorf("%q: %q is not an agent id", it, id)
		}
		for i := 0; i < n; i++ {
			out = append(out, id)
		}
		if len(out) > MaxLanes {
			return nil, fmt.Errorf("a race has at most %d lanes", MaxLanes)
		}
	}
	return out, nil
}
