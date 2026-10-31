package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
)

// Lesson recall (OMX-18): after each AI tool call of a chat turn, the
// saved lessons are matched against the call (its paths and words; local
// and cheap). A lesson that matches and was not offered in this chat yet
// is added to the tool result, so the model sees it before its next
// step, and the chat shows "recalled: <title>". With ai_memory_recall
// "check" the quick model is asked first whether the lesson applies (one
// small request per new match; opt-in). "off" turns recall off. It works
// only while memory is on.

// aiRecallChip starts the chat line of a recalled lesson.
const aiRecallChip = "💡 recalled: "

// aiRecallMax caps the lessons one call can recall.
const aiRecallMax = 2

// aiRecallTimeout bounds one quick-model check.
var aiRecallTimeout = 15 * time.Second

// Recall modes (config.json ai_memory_recall).
const (
	aiRecallOn    = "on" // the default
	aiRecallCheck = "check"
	aiRecallOff   = "off"
)

// aiRecallMode reads ai_memory_recall.
func aiRecallMode() string {
	var s struct {
		Recall string `json:"ai_memory_recall"`
	}
	if path, err := ai.ConfigPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	switch s.Recall {
	case aiRecallCheck, aiRecallOff:
		return s.Recall
	}
	return aiRecallOn
}

// aiRecallState is the recall memory of the chats: the lessons offered
// per chat and the chips waiting for the chat.
type aiRecallState struct {
	mu    sync.Mutex
	seen  map[string]map[string]bool // chat id → lesson keys
	chips []string
}

// aiRecaller is one turn's recall: a snapshot of the lessons and the
// optional quick check.
type aiRecaller struct {
	st      *aiRecallState
	conv    string
	lessons []aimemory.Lesson
	check   func(ctx context.Context, l aimemory.Lesson, call string) bool // nil: no check
}

// aiRecallTurn prepares recall for a chat turn about text, or nil when
// recall is off, memory is off or there are no lessons. The lessons the
// turn's prompt already has count as seen.
func (m *Model) aiRecallTurn(root, text string) *aiRecaller {
	s := m.aiS()
	mode := aiRecallMode()
	if mode == aiRecallOff || aiMemoryState() != aiMemoryOn {
		return nil
	}
	ls, err := s.aiMemoryLoad(root)
	if err != nil || len(ls) == 0 {
		return nil
	}
	if s.recall == nil {
		s.recall = &aiRecallState{seen: map[string]map[string]bool{}}
	}
	st := s.recall
	st.mu.Lock()
	seen := st.seen[s.conv.ID]
	if seen == nil {
		seen = map[string]bool{}
		st.seen[s.conv.ID] = seen
	}
	for _, l := range aimemory.Select(ls, aimemory.Hints{Text: text}, aiMemoryPromptBytes, aiMemoryPromptMax) {
		seen[l.Key()] = true
	}
	st.mu.Unlock()
	r := &aiRecaller{st: st, conv: s.conv.ID, lessons: ls}
	if mode == aiRecallCheck {
		if slot, err := s.resolveSlot(ai.SlotQuick); err == nil {
			p, model := slot.p, slot.cfg.Model
			r.check = func(ctx context.Context, l aimemory.Lesson, call string) bool {
				ctx, cancel := context.WithTimeout(ctx, aiRecallTimeout)
				defer cancel()
				req := ai.Request{Model: model, Messages: []ai.Message{
					{Role: ai.RoleSystem, Content: "You check whether a saved lesson applies to a tool call. Answer only yes or no."},
					{Role: ai.RoleUser, Content: ai.Redact("Lesson: " + l.Title + ": " + l.Text + "\n\nTool call: " + call + "\n\nDoes the lesson apply?")},
				}}
				out, _ := ai.Collect(p.Stream(ctx, req))
				return strings.HasPrefix(strings.ToLower(strings.TrimSpace(out)), "yes")
			}
		}
	}
	return r
}

// aiRecallCall is the text and paths of a call for matching.
func aiRecallCall(tool string, args map[string]any) (string, []string) {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(tool)
	var paths []string
	for _, k := range keys {
		switch v := args[k].(type) {
		case string:
			if k == "path" || k == "file" || k == "dir" {
				paths = append(paths, v)
			}
			if k != "content" && k != "new_text" && k != "old_text" { // file text is not the call's intent
				b.WriteString(" " + v)
			}
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok && (k == "paths" || k == "files") {
					paths = append(paths, s)
				}
			}
		}
	}
	text := b.String()
	if len(text) > 2000 {
		text = text[:2000]
	}
	return text, paths
}

// wrap adds recall to the Run of every tool.
func (r *aiRecaller) wrap(tools []ai.Tool) []ai.Tool {
	if r == nil {
		return tools
	}
	out := make([]ai.Tool, len(tools))
	for i, t := range tools {
		out[i] = t
		if t.Run == nil || t.Name == "skill_load" {
			continue
		}
		run, name := t.Run, t.Name
		out[i].Run = func(ctx context.Context, args map[string]any) (string, error) {
			res, err := run(ctx, args)
			if err != nil {
				return res, err
			}
			return res + r.after(ctx, name, args), nil
		}
	}
	return out
}

// after matches the lessons against one finished call and returns the
// text added to its result ("" when nothing new matches).
func (r *aiRecaller) after(ctx context.Context, tool string, args map[string]any) string {
	text, paths := aiRecallCall(tool, args)
	r.st.mu.Lock()
	seen := r.st.seen[r.conv]
	hits := aimemory.Recall(r.lessons, aimemory.Hints{Text: text, Paths: paths}, seen, aiRecallMax)
	for _, l := range hits {
		seen[l.Key()] = true // asked once per chat, whatever the check says
	}
	r.st.mu.Unlock()
	var b strings.Builder
	for _, l := range hits {
		if r.check != nil && !r.check(ctx, l, text) {
			continue
		}
		title := aimemory.CleanLine(l.Title, aimemory.MaxTitleRunes)
		fmt.Fprintf(&b, "\n\n[Recalled lesson from an earlier session; use it if it fits] %s: %s",
			title, aimemory.CleanLine(l.Text, aimemory.MaxTextRunes))
		r.st.mu.Lock()
		r.st.chips = append(r.st.chips, aiRecallChip+title)
		r.st.mu.Unlock()
	}
	return ai.Redact(b.String())
}

// aiTakeRecallChips returns the chips waiting for the chat.
func (s *aiState) aiTakeRecallChips() []string {
	if s.recall == nil {
		return nil
	}
	s.recall.mu.Lock()
	defer s.recall.mu.Unlock()
	c := s.recall.chips
	s.recall.chips = nil
	return c
}
