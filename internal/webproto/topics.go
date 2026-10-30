package webproto

import (
	"errors"
	"strconv"
	"strings"
)

// Phase is the release phase a topic or an op belongs to.
type Phase string

// Phases.
const (
	PhaseV1 Phase = "v1"
	PhaseV2 Phase = "v2"
)

// TopicDef describes one topic name.
type TopicDef struct {
	Name  string
	Keyed bool // "buf:<id>", "term:<buf>": the key is an nvim buffer number
	Phase Phase
}

var topicDefs = []TopicDef{
	{Name: "status", Phase: PhaseV1},
	{Name: "toasts", Phase: PhaseV1},
	{Name: "chats", Phase: PhaseV1},
	{Name: "chat", Phase: PhaseV1},
	{Name: "cards", Phase: PhaseV1},
	{Name: "tree", Phase: PhaseV1},
	{Name: "bufs", Phase: PhaseV1},
	{Name: "buf", Keyed: true, Phase: PhaseV1},
	{Name: "terms", Phase: PhaseV1},
	{Name: "term", Keyed: true, Phase: PhaseV1},
	{Name: "git", Phase: PhaseV1},
	{Name: "tasks", Phase: PhaseV1},
	{Name: "runs", Phase: PhaseV1},
	{Name: "db", Phase: PhaseV2},
	{Name: "containers", Phase: PhaseV2},
	{Name: "mcp", Phase: PhaseV2},
	{Name: "settings", Phase: PhaseV2},
	{Name: "diagnostics", Phase: PhaseV2},
	{Name: "tests", Phase: PhaseV2},
	{Name: "debug", Phase: PhaseV2},
}

// Topics lists every topic the protocol knows.
func Topics() []TopicDef { return append([]TopicDef(nil), topicDefs...) }

func topicDef(name string) (TopicDef, bool) {
	for _, d := range topicDefs {
		if d.Name == name {
			return d, true
		}
	}
	return TopicDef{}, false
}

// Topic is a parsed topic: "buf:12" is {Name: "buf", Key: "12"}.
type Topic struct {
	Name string
	Key  string
}

// String formats the topic as it goes on the wire.
func (t Topic) String() string {
	if t.Key == "" {
		return t.Name
	}
	return t.Name + ":" + t.Key
}

// ErrBadTopic is returned for a topic name the protocol does not know, or
// a key that is missing, extra or not a buffer number.
var ErrBadTopic = errors.New("webproto: bad topic")

// ParseTopic parses and checks a topic name from the wire.
func ParseTopic(s string) (Topic, error) {
	name, key, hasKey := strings.Cut(s, ":")
	d, ok := topicDef(name)
	if !ok || hasKey != d.Keyed {
		return Topic{}, ErrBadTopic
	}
	if d.Keyed && !isBufNumber(key) {
		return Topic{}, ErrBadTopic
	}
	return Topic{Name: name, Key: key}, nil
}

// isBufNumber accepts a positive decimal int32 without a leading zero.
func isBufNumber(s string) bool {
	if s == "" || s[0] == '0' || len(s) > 10 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.ParseInt(s, 10, 32)
	return err == nil && n > 0
}
