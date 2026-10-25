package aimemory

import (
	"encoding/json"
	"errors"
	"path"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// Limits of one lesson from a model reply.
const (
	MaxPerTurn    = 3
	MaxTitleRunes = 80
	MaxTextRunes  = 300
	maxPaths      = 5
	maxPathLen    = 200
)

// Redacted is the mark the secret redaction leaves; a lesson holding it
// is dropped (it was about a secret).
const Redacted = "[REDACTED]"

// ErrNoLessons is a reply without a lessons JSON object.
var ErrNoLessons = errors.New("the reply has no lessons JSON")

// ParseReply reads the lessons of a model reply: the first fenced code
// block (or the whole reply) must be {"lessons":[{"title","text","paths"}]}.
// At most three are kept; titles and texts are made one clean line and
// cut; paths must stay inside the repository; a lesson that is empty or
// holds [REDACTED] is dropped. The lessons have no id and no date.
func ParseReply(text string) ([]Lesson, error) {
	body, ok := ai.FirstCodeBlock(text)
	if !ok {
		body = strings.TrimSpace(text)
	}
	var reply struct {
		Lessons []struct {
			Title string   `json:"title"`
			Text  string   `json:"text"`
			Paths []string `json:"paths"`
		} `json:"lessons"`
	}
	dec := json.NewDecoder(strings.NewReader(body))
	if err := dec.Decode(&reply); err != nil || reply.Lessons == nil {
		return nil, ErrNoLessons
	}
	if len(reply.Lessons) > MaxPerTurn {
		reply.Lessons = reply.Lessons[:MaxPerTurn]
	}
	var out []Lesson
	for _, r := range reply.Lessons {
		l := Lesson{Title: CleanLine(r.Title, MaxTitleRunes), Text: CleanLine(r.Text, MaxTextRunes)}
		l.Title = strings.ReplaceAll(l.Title, "{#", "{ #") // never a fake id
		if l.Title == "" || l.Text == "" || strings.HasPrefix(l.Text, "paths:") ||
			strings.Contains(l.Title, Redacted) || strings.Contains(l.Text, Redacted) {
			continue
		}
		for _, p := range r.Paths {
			if p, ok := cleanPath(p); ok && len(l.Paths) < maxPaths {
				l.Paths = append(l.Paths, p)
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// CleanLine makes s one safe line (no escapes, control or bidi
// characters) of at most n runes.
func CleanLine(s string, n int) string {
	s = agentdag.Clean(s)
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}

// cleanPath keeps a workspace-relative path: not absolute, no "..", no
// commas (the file's list separator), no secrets.
func cleanPath(p string) (string, bool) {
	p = agentdag.Clean(p)
	if p == "" || len(p) > maxPathLen || strings.ContainsAny(p, ",\\") || strings.Contains(p, Redacted) ||
		strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || (len(p) > 1 && p[1] == ':') {
		return "", false
	}
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}
