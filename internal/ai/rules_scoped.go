package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/search"
)

// ScopedRule is a rules text for some files only (OMX-9).
type ScopedRule struct {
	File  string   // relative to the root, slash-separated
	Dir   string   // a folder rule: files below this folder ("" for a glob rule)
	Globs []string // a glob rule: files matching any of these
	Text  string   // cleaned, front matter removed
}

// RulesScopedMaxBytes caps one scoped rule's text in a tool result.
const RulesScopedMaxBytes = 4 << 10

// rulesScopedMax caps how many scoped rule files are read.
const rulesScopedMax = 64

// ClaudeRulesDir holds the conditional rules files (*.md, *.mdc).
const ClaudeRulesDir = ".claude/rules"

type claudeRule struct {
	ScopedRule
	size int
}

// loadClaudeRules reads .claude/rules/*.md(c). A file with globs (or
// paths) in its front matter is scoped, unless alwaysApply is true; a
// file without them always applies.
func loadClaudeRules(root, realRoot string) []claudeRule {
	dir := filepath.Join(root, filepath.FromSlash(ClaudeRulesDir))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []claudeRule
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".md") || strings.HasSuffix(n, ".mdc")) || len(out) == rulesScopedMax {
			continue
		}
		text, size, ok := readRulesFile(realRoot, filepath.Join(dir, n))
		if !ok {
			continue
		}
		globs, always, body := rulesFrontMatter(text)
		if body = strings.Trim(body, " \t\n"); body == "" {
			continue
		}
		if always {
			globs = nil
		}
		out = append(out, claudeRule{ScopedRule{File: ClaudeRulesDir + "/" + n, Globs: globs, Text: body}, size})
	}
	return out
}

// rulesFrontMatter splits a "---" front matter off text and reads its
// globs / paths (a comma list, a [..] list or "- item" lines) and
// alwaysApply.
func rulesFrontMatter(text string) (globs []string, always bool, body string) {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, false, text
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return nil, false, text
	}
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	inList := false
	addGlobs := func(v string) {
		v = strings.Trim(strings.TrimSpace(v), "[]")
		for _, g := range strings.Split(v, ",") {
			if g = strings.Trim(strings.TrimSpace(g), `"'`); g != "" {
				globs = append(globs, g)
			}
		}
	}
	for _, l := range strings.Split(head, "\n") {
		t := strings.TrimSpace(l)
		if item, ok := strings.CutPrefix(t, "- "); ok && inList {
			addGlobs(item)
			continue
		}
		inList = false
		k, v, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "globs", "paths":
			if strings.TrimSpace(v) == "" {
				inList = true
			}
			addGlobs(v)
		case "alwaysApply":
			always = strings.TrimSpace(v) == "true"
		}
	}
	return globs, always, body
}

// loadFolderRules finds AGENTS.md and CLAUDE.md below the root (ignored,
// hidden and dependency folders are skipped).
func loadFolderRules(root, realRoot string, bytes *int) []ScopedRule {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	files, _, err := search.Files(ctx, root, []string{"AGENTS.md", "CLAUDE.md"}, rulesScopedMax)
	if err != nil {
		return nil
	}
	var out []ScopedRule
	for _, f := range files {
		dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(f)))
		base := filepath.Base(f)
		if dir == "." || (base != "AGENTS.md" && base != "CLAUDE.md") {
			continue // the root ones are in the prompt
		}
		text, size, ok := readRulesFile(realRoot, filepath.Join(root, filepath.FromSlash(f)))
		if !ok {
			continue
		}
		*bytes += size
		out = append(out, ScopedRule{File: f, Dir: dir, Text: text})
	}
	return out
}

// ScopedFor returns the scoped rules for the file rel (relative to the
// root), in File order.
func (r Rules) ScopedFor(rel string) []ScopedRule {
	rel = filepath.ToSlash(rel)
	var out []ScopedRule
	for _, s := range r.Scoped {
		if s.Dir != "" {
			if strings.HasPrefix(rel, s.Dir+"/") {
				out = append(out, s)
			}
			continue
		}
		for _, g := range s.Globs {
			if search.MatchGlob(g, rel) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// Preview is the rules text the trust card shows: the prompt text, or
// the scoped rules when there is none.
func (r Rules) Preview() string {
	if r.Text != "" {
		return r.Text
	}
	var parts []string
	for _, s := range r.Scoped {
		parts = append(parts, "# "+s.File+"\n"+s.Text)
	}
	return strings.Join(parts, "\n\n")
}

// cutRules cuts text to max bytes at a line end and adds a note.
func cutRules(text string, max int) string {
	cut := text[:max]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	if max == RulesMaxBytes {
		return strings.TrimRight(cut, " \t\n") + "\n" + rulesCutNote
	}
	return strings.TrimRight(cut, " \t\n") + "\n(cut)"
}

// RulesInjector adds scoped rules to read_file results: each rule once
// per chat, at most RulesMaxBytes in all. Keep one per chat.
type RulesInjector struct {
	mu   sync.Mutex
	seen map[string]bool
	used int
}

// Inject returns the text to add after reading the file rel ("" for
// none). r must be rules the user trusts.
func (j *RulesInjector) Inject(r Rules, rel string) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	var b strings.Builder
	for _, s := range r.ScopedFor(rel) {
		if j.seen[s.File] || j.used >= RulesMaxBytes {
			continue
		}
		if j.seen == nil {
			j.seen = map[string]bool{}
		}
		j.seen[s.File] = true
		text := s.Text
		if len(text) > RulesScopedMaxBytes {
			text = cutRules(text, RulesScopedMaxBytes)
		}
		j.used += len(text)
		b.WriteString("\n\n[Project rules for this file, from " + s.File + ". Follow them unless they conflict with the user's request.]\n" + Redact(text))
	}
	return b.String()
}
