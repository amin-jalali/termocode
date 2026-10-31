package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Project rules (OM-1): a repository's working instructions for AI
// agents. AGENTS.md and .termocode/rules.md in the workspace root are read
// at the start of every chat turn (no cache, no watcher) and, once the
// user trusts that exact text, go into the system prompt. The text is
// untrusted input: it never gets tools and never passes a confirm card.

// RulesFiles are the rules files, in prompt order, relative to the root.
var RulesFiles = []string{"AGENTS.md", "CLAUDE.md", ".termocode/rules.md"}

// RulesMaxBytes caps the rules text that goes into a prompt (~4k tokens).
const RulesMaxBytes = 16 << 10

// rulesReadMax is how much of one file is read at most.
const rulesReadMax = 4 * RulesMaxBytes

// rulesCutNote ends a text that was cut at RulesMaxBytes.
const rulesCutNote = "(cut at 16 KB)"

// Rules is what LoadProjectRules found.
type Rules struct {
	Files     []string // the files that had text, relative to the root (scoped ones last)
	Text      string   // cleaned and capped; each file under "# <file>" when there are several
	Bytes     int      // the files' size on disk
	Truncated bool     // Text was cut at RulesMaxBytes
	Hash      string   // sha256 of Text and every Scoped rule ("" when there is no text)
	// Scoped are the rules for some files only (OMX-9): AGENTS.md /
	// CLAUDE.md below the root and .claude/rules files with globs. They
	// are added to a read_file result, once per chat (RulesInjector).
	Scoped []ScopedRule
}

// LoadProjectRules reads the rules files of root. A missing, empty or
// unreadable file, and a symlink that leaves root, are skipped.
func LoadProjectRules(root string) Rules {
	var r Rules
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return r
	}
	var parts, names []string
	add := func(name, text string, size int) {
		r.Files = append(r.Files, name)
		r.Bytes += size
		parts, names = append(parts, text), append(names, name)
	}
	for _, name := range RulesFiles {
		text, size, ok := readRulesFile(realRoot, filepath.Join(root, filepath.FromSlash(name)))
		if !ok {
			continue
		}
		add(name, text, size)
	}
	for _, cr := range loadClaudeRules(root, realRoot) {
		if len(cr.Globs) == 0 {
			add(cr.File, cr.Text, cr.size)
		} else {
			r.Scoped = append(r.Scoped, cr.ScopedRule)
			r.Bytes += cr.size
		}
	}
	r.Scoped = append(r.Scoped, loadFolderRules(root, realRoot, &r.Bytes)...)
	sort.Slice(r.Scoped, func(i, j int) bool { return r.Scoped[i].File < r.Scoped[j].File })
	for _, sr := range r.Scoped {
		r.Files = append(r.Files, sr.File)
	}
	switch len(parts) {
	case 0:
	case 1:
		r.Text = parts[0]
	default:
		var b strings.Builder
		for i, p := range parts {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString("# " + names[i] + "\n" + p)
		}
		r.Text = b.String()
	}
	if len(r.Text) > RulesMaxBytes {
		r.Text = cutRules(r.Text, RulesMaxBytes)
		r.Truncated = true
	}
	if r.Text == "" && len(r.Scoped) == 0 {
		return r
	}
	h := sha256.New()
	h.Write([]byte(r.Text))
	for _, sr := range r.Scoped {
		h.Write([]byte("\x00" + sr.File + "\x00" + strings.Join(sr.Globs, ",") + "\x00" + sr.Text))
	}
	r.Hash = hex.EncodeToString(h.Sum(nil))
	return r
}

// readRulesFile reads one rules file: a regular file inside realRoot,
// cleaned; ok=false when it has no text.
func readRulesFile(realRoot, path string) (text string, size int, ok bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", 0, false
	}
	if rel, err := filepath.Rel(realRoot, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", 0, false
	}
	f, err := os.Open(real)
	if err != nil {
		return "", 0, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", 0, false
	}
	b, err := io.ReadAll(io.LimitReader(f, rulesReadMax))
	if err != nil {
		return "", 0, false
	}
	text = strings.Trim(cleanRulesText(string(b)), " \t\n")
	if text == "" {
		return "", 0, false
	}
	return text, int(st.Size()), true
}

// cleanRulesText removes escape sequences, control characters (but not
// line breaks and tabs), carriage returns and bidi controls, so the
// rules can neither draw on the terminal nor hide text from the user.
func cleanRulesText(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case rulesBidi(r), unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func rulesBidi(r rune) bool {
	switch {
	case r == 0x061C, r == 0x200E, r == 0x200F:
		return true
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}
