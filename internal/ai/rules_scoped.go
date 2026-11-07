package ai

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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
	// Mode is "" for a folder or glob rule, RuleModeManual (used when the
	// user names it as #Name) or RuleModeAuto (offered to the model by
	// Description, loaded with rule_load). CMP-82.
	Mode        string
	Name        string
	Description string
	// Review is a folder file's review-only rules (CMP-37), cut out of Text.
	Review string
}

// The inclusion modes of a .claude/rules file (front matter
// "inclusion: manual" / "inclusion: auto").
const (
	RuleModeManual = "manual"
	RuleModeAuto   = "auto"
)

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
		fm := rulesFrontMatterAll(text)
		body := strings.Trim(fm.body, " \t\n")
		if body == "" {
			continue
		}
		globs := fm.globs
		if fm.always {
			globs = nil
		}
		sr := ScopedRule{File: ClaudeRulesDir + "/" + n, Globs: globs, Text: body}
		switch fm.inclusion { // CMP-82
		case RuleModeManual, RuleModeAuto:
			sr.Globs, sr.Mode, sr.Name = nil, RuleModeManual, ruleName(fm.name, n)
			if fm.inclusion == RuleModeAuto && fm.desc != "" {
				sr.Mode, sr.Description = RuleModeAuto, fm.desc
			}
		}
		out = append(out, claudeRule{sr, size})
	}
	return out
}

// ruleFront is a rules file's front matter: globs, alwaysApply and the
// CMP-82 keys inclusion, name and description.
type ruleFront struct {
	globs                 []string
	always                bool
	inclusion, name, desc string
	body                  string
}

var ruleNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)

// ruleName is the #name of a rule: the front matter name when it is a
// short lower-case word, else the file name without its extension
// (lower case, spaces as -, other characters dropped).
func ruleName(front, file string) string {
	if n := strings.ToLower(strings.TrimSpace(front)); ruleNameRe.MatchString(n) {
		return n
	}
	base := strings.TrimSuffix(strings.TrimSuffix(file, ".mdc"), ".md")
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteByte('-')
		}
	}
	n := strings.Trim(b.String(), "-_")
	if len(n) > 48 {
		n = n[:48]
	}
	if n == "" {
		return "rule"
	}
	return n
}

// rulesFrontMatterAll splits a "---" front matter off text and reads its
// globs / paths / fileMatchPattern (a comma list, a [..] list or "- item"
// lines), alwaysApply, inclusion, name and description.
func rulesFrontMatterAll(text string) (fm ruleFront) {
	fm.body = text
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return fm
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return fm
	}
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		fm.body = body[i+1:]
	} else {
		fm.body = ""
	}
	inList := false
	addGlobs := func(v string) {
		v = strings.Trim(strings.TrimSpace(v), "[]")
		for _, g := range strings.Split(v, ",") {
			if g = strings.Trim(strings.TrimSpace(g), `"'`); g != "" {
				fm.globs = append(fm.globs, g)
			}
		}
	}
	unquote := func(v string) string { return strings.Trim(strings.TrimSpace(v), `"'`) }
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
		case "globs", "paths", "fileMatchPattern":
			if strings.TrimSpace(v) == "" {
				inList = true
			}
			addGlobs(v)
		case "alwaysApply":
			fm.always = strings.TrimSpace(v) == "true"
		case "inclusion":
			fm.inclusion = strings.ToLower(unquote(v))
		case "name":
			fm.name = unquote(v)
		case "description":
			fm.desc = unquote(v)
		}
	}
	return fm
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
		rest, rev := SplitReviewRules(text)
		out = append(out, ScopedRule{File: f, Dir: dir, Text: rest, Review: rev})
	}
	return out
}

// ScopedFor returns the scoped rules for the file rel (relative to the
// root), in File order.
func (r Rules) ScopedFor(rel string) []ScopedRule {
	rel = filepath.ToSlash(rel)
	var out []ScopedRule
	for _, s := range r.Scoped {
		if s.Mode != "" { // named or offered, never by file (CMP-82)
			continue
		}
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
	var parts []string
	if r.Text != "" {
		parts = append(parts, r.Text)
	} else {
		for _, s := range r.Scoped {
			if s.Text != "" {
				parts = append(parts, "# "+s.File+"\n"+s.Text)
			}
		}
	}
	// The review-only rules (CMP-37) are part of the same trust answer.
	if r.Review != "" {
		parts = append(parts, "# Review rules\n"+r.Review)
	}
	for _, s := range r.Scoped {
		if s.Review != "" {
			parts = append(parts, "# Review rules, "+s.File+"\n"+s.Review)
		}
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
		if s.Text == "" || j.seen[s.File] || j.used >= RulesMaxBytes {
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
