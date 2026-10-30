package ai

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Inline suggestion policy: when to ask the model at all, and which of
// its answers are worth showing. Modeled on what Copilot / Supermaven /
// Zed do: complete what the user is typing, never invent new top-level
// code on an empty line, never repeat code that is already there.

// InlineContext is the caret context the policy looks at. Before / After
// are the windowed buffer text around the caret; LineBefore / LineAfter
// split the current line.
type InlineContext struct {
	Filetype              string
	Before, After         string
	LineBefore, LineAfter string
}

// InlineMaxLines caps a multi-line ghost text.
const InlineMaxLines = 8

// ShouldAutoTrigger reports whether an automatic (debounced) request is
// worth sending here. why names the rule that said no. A manual request
// (Alt+\) skips this check.
func ShouldAutoTrigger(c InlineContext) (ok bool, why string) {
	if strings.TrimSpace(c.Before+c.After) == "" {
		return false, "empty buffer"
	}
	if r, _ := utf8.DecodeRuneInString(c.LineAfter); isIdentRune(r) {
		return false, "mid-word"
	}
	if !onlyClosers(strings.TrimSpace(c.LineAfter)) {
		return false, "code right of the caret"
	}
	if inLineComment(c.Filetype, c.LineBefore) {
		return false, "in a comment"
	}
	if !isBlank(c.LineBefore) {
		return true, ""
	}
	// Empty line: this is where models invent "a new function". Only
	// suggest inside a body that is still open.
	if indentOf(c.LineBefore) == "" {
		return false, "empty top-level line"
	}
	prev := prevLines(c.Before)
	if len(prev) == 0 {
		return false, "empty top-level line"
	}
	if isBlank(prev[len(prev)-1]) {
		return false, "second empty line"
	}
	if closesScope(prev[len(prev)-1]) {
		return false, "right after a closed block"
	}
	return true, ""
}

// NeedsLongerPause reports whether the caret sits on an empty line, where
// a suggestion should wait for a longer pause than the normal debounce.
func NeedsLongerPause(c InlineContext) bool {
	return isBlank(c.LineBefore) && isBlank(c.LineAfter)
}

// FilterInlineCompletion cleans a parsed model answer (InsertText from
// ParseInlineResponse) for the caret context. ok=false: show nothing.
//
//   - a new declaration (func / def / class / type …) the user did not
//     start is dropped; later ones are cut off;
//   - mid-statement only the first line is kept, unless that line opens a
//     block;
//   - trailing lines that repeat the code below the caret are cut, and an
//     answer that only repeats the next or previous line is dropped;
//   - closers-only answers are dropped; long answers are capped.
func FilterInlineCompletion(c InlineContext, text string, manual bool) (string, bool) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(text) == "" {
		return "", false
	}
	full0 := fullFirstLine(c.LineBefore, lines[0])
	if !manual && isBlank(c.LineBefore) && isDecl(full0) {
		return "", false
	}
	for i := 1; i < len(lines); i++ {
		if isDecl(lines[i]) {
			lines = lines[:i]
			break
		}
	}
	if !manual && len(lines) > 1 && !isBlank(c.LineBefore) && !opensBlock(full0) {
		lines = lines[:1]
	}
	if len(lines) > InlineMaxLines {
		lines = lines[:InlineMaxLines]
	}
	for len(lines) > 1 && isBlank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	// Cut trailing lines that are already below the caret.
	full := append([]string{full0}, lines[1:]...)
	next := nextLines(c.After)
	for k := min(len(full), len(next)); k > 0; k-- {
		if sameLines(full[len(full)-k:], next[:k]) {
			lines = lines[:len(lines)-k]
			full = full[:len(full)-k]
			break
		}
	}
	if len(lines) == 0 {
		return "", false
	}
	out := strings.Join(lines, "\n")
	if onlyClosers(strings.TrimSpace(out)) {
		return "", false
	}
	if len(full) == 1 {
		if prev := lastNonBlank(prevLines(c.Before)); prev != "" && strings.TrimSpace(prev) == strings.TrimSpace(full0) {
			return "", false
		}
	}
	return out, true
}

// InlineCache lets the user "type through" a shown suggestion: while the
// typed characters match the start of the ghost text, the rest is shown
// again without a new request.
type InlineCache struct {
	set             bool
	buf, row        int
	lineBefore, ins string
}

// Remember stores a shown continuation (never a rewrite).
func (c *InlineCache) Remember(buf, row int, lineBefore, insert string) {
	*c = InlineCache{set: true, buf: buf, row: row, lineBefore: lineBefore, ins: insert}
}

// Match returns what is left of the remembered suggestion after the user
// typed more of it on the same line. ok=false: ask the model.
func (c *InlineCache) Match(buf, row int, lineBefore string) (string, bool) {
	if !c.set || c.buf != buf || c.row != row || !strings.HasPrefix(lineBefore, c.lineBefore) {
		return "", false
	}
	typed := lineBefore[len(c.lineBefore):]
	if !strings.HasPrefix(c.ins, typed) || len(typed) >= len(c.ins) {
		return "", false
	}
	rest := c.ins[len(typed):]
	if strings.TrimSpace(rest) == "" {
		return "", false
	}
	return rest, true
}

// Empty reports whether nothing is remembered.
func (c *InlineCache) Empty() bool { return !c.set }

// Reset forgets the remembered suggestion.
func (c *InlineCache) Reset() { *c = InlineCache{} }

// ---- helpers ----

var declRe = lazyre.New(`^\s*(export\s+)?(default\s+)?(async\s+)?(pub(\([^)]*\))?\s+)?((static|private|public|protected|abstract|final|override|local)\s+)*(func|def|class|type|fn|function|interface|struct|impl|trait|enum|module|object)\b`)

func isDecl(line string) bool { return declRe.MatchString(line) }

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

func indentOf(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

// onlyClosers: s is made only of closing brackets, quotes and separators.
func onlyClosers(s string) bool {
	return strings.Trim(s, ")]}>\"'`;,: \t\n") == ""
}

// prevLines returns the lines before the caret line.
func prevLines(before string) []string {
	lines := strings.Split(before, "\n")
	return lines[:len(lines)-1]
}

// nextLines returns the lines after the caret line.
func nextLines(after string) []string {
	lines := strings.Split(after, "\n")
	return lines[1:]
}

func lastNonBlank(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if !isBlank(lines[i]) {
			return lines[i]
		}
	}
	return ""
}

func sameLines(a, b []string) bool {
	for i := range a {
		if strings.TrimRight(a[i], " \t") != strings.TrimRight(b[i], " \t") {
			return false
		}
	}
	return true
}

// fullFirstLine is the caret line as it would read after accepting.
func fullFirstLine(lineBefore, first string) string {
	r := ResolveInlineCompletion(lineBefore, "", first)
	return lineBefore[:len(lineBefore)-r.DeleteBefore] + r.Insert
}

// closesScope: the line ends a block (and does not open a new one, like
// "} else {").
func closesScope(line string) bool {
	t := strings.TrimSpace(line)
	if opensBlock(t) {
		return false
	}
	if strings.HasPrefix(t, "}") || strings.HasPrefix(t, ")") || strings.HasPrefix(t, "]") {
		return true
	}
	switch strings.TrimRight(t, ",;)") {
	case "end", "fi", "done", "esac", "endif", "endfunction":
		return true
	}
	return false
}

// opensBlock: the line ends with something that wants a body after it.
func opensBlock(line string) bool {
	t := strings.TrimRight(line, " \t")
	for _, s := range []string{"{", "(", "[", ":", "=>", "->", " do", " then"} {
		if strings.HasSuffix(t, s) {
			return true
		}
	}
	return false
}

var (
	hashCommentFT = map[string]bool{"python": true, "sh": true, "bash": true, "zsh": true, "fish": true,
		"ruby": true, "yaml": true, "toml": true, "make": true, "perl": true, "r": true, "conf": true,
		"dockerfile": true, "elixir": true, "nim": true, "cmake": true}
	dashCommentFT = map[string]bool{"lua": true, "sql": true, "haskell": true, "ada": true}
)

// inLineComment: the caret is inside a line comment (outside strings).
func inLineComment(ft, lineBefore string) bool {
	marker := "//"
	switch {
	case hashCommentFT[ft]:
		marker = "#"
	case dashCommentFT[ft]:
		marker = "--"
	}
	var quote byte
	for i := 0; i < len(lineBefore); i++ {
		ch := lineBefore[i]
		if quote != 0 {
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '`' || (ch == '\'' && ft != "rust") {
			quote = ch
			continue
		}
		if strings.HasPrefix(lineBefore[i:], marker) {
			return true
		}
	}
	return false
}
