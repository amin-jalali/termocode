package ai

import (
	"strings"
)

// Inline ghost-text completion helpers — a port of mobocode's
// inline_completion.dart (ADR 0013 there): ghost text is resolved as
// continuation-vs-rewrite, never blind insert-at-caret.

// InlineCompletion is a parsed model answer: the code to insert plus any
// import lines the code needs.
type InlineCompletion struct {
	InsertText  string
	ImportLines []string
}

// ResolvedCompletion says how to apply a completion at the caret: delete
// DeleteBefore bytes left of the caret, then insert Insert.
type ResolvedCompletion struct {
	DeleteBefore int
	Insert       string
}

// affixOverlap is the length of the longest suffix of a that is also a
// prefix of b. Byte-based: a suffix that starts mid-rune begins with a
// continuation byte and can never equal a valid UTF-8 prefix.
func affixOverlap(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for k := max; k > 0; k-- {
		if a[len(a)-k:] == b[:k] {
			return k
		}
	}
	return 0
}

// ResolveInlineCompletion decides how to apply completion at a caret that
// splits the current line into before / after:
//
//   - continuation — the completion restates a suffix of before
//     ("fmt.Pri" + "Println(…)"): append only the new remainder;
//   - rewrite — no overlap ("print salam" → `fmt.Println("salam")`):
//     replace the typed line content, keeping indentation.
//
// A trailing overlap with after (a ")" the model repeats) is trimmed first.
// A blank (indent-only) before is a pure insertion.
func ResolveInlineCompletion(before, after, completion string) ResolvedCompletion {
	out := completion
	if after != "" && strings.HasSuffix(out, after) {
		out = out[:len(out)-len(after)]
	}
	if ov := affixOverlap(before, out); ov > 0 {
		return ResolvedCompletion{Insert: out[ov:]}
	}
	indent := len(before) - len(strings.TrimLeft(before, " \t"))
	return ResolvedCompletion{DeleteBefore: len(before) - indent, Insert: out}
}

// CursorMarker is spliced into the prompt at the caret.
const CursorMarker = "<CURSOR>"

const inlineSystemPrompt = "You are an inline code completion engine. The file is given with <CURSOR> " +
	"at the caret. Output the COMPLETE code for the statement or expression the " +
	"user is typing — you may rewrite the partial word before the cursor into " +
	"its full intended form. Example: with `print` before the cursor, output " +
	"`fmt.Println(\"Hello, World!\")`. Code only: no prose, no markdown fences. " +
	"Keep it short: finish the current statement or block, at most a few lines.\n" +
	"For each package/module your code uses that is not already imported, put " +
	"one line at the very TOP (one per import), exactly: `IMPORT: <the import " +
	"statement>` — e.g. for Go: `IMPORT: import \"fmt\"`."

// Size caps for the inline prompt (bytes of text before / after the caret).
const (
	InlineMaxBefore = 6000
	InlineMaxAfter  = 1500
)

// CapContext trims before from the left and after from the right to the
// byte caps, cutting at line boundaries where possible.
func CapContext(before, after string, maxBefore, maxAfter int) (string, string) {
	if maxBefore > 0 && len(before) > maxBefore {
		before = before[len(before)-maxBefore:]
		if i := strings.IndexByte(before, '\n'); i >= 0 && i < len(before)-1 {
			before = before[i+1:]
		}
	}
	if maxAfter > 0 && len(after) > maxAfter {
		after = after[:maxAfter]
		if i := strings.LastIndexByte(after, '\n'); i > 0 {
			after = after[:i]
		}
	}
	return before, after
}

// BuildInlinePrompt returns the messages for one completion request.
// before/after are the windowed buffer text around the caret.
func BuildInlinePrompt(filetype, path, before, after string) []Message {
	before, after = CapContext(before, after, InlineMaxBefore, InlineMaxAfter)
	lang := ""
	if filetype != "" {
		lang = " (language: " + filetype + ")"
	}
	name := ""
	if path != "" {
		name = " " + baseName(path)
	}
	return []Message{
		{Role: RoleSystem, Content: inlineSystemPrompt},
		{Role: RoleUser, Content: "File" + name + lang + ":\n" + before + CursorMarker + after},
	}
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// ParseInlineResponse cleans a raw model answer: strips a wrapping ```
// fence, pulls every IMPORT: line into ImportLines (deduped), trims one
// leading newline. ok=false when no code is left.
func ParseInlineResponse(raw string) (InlineCompletion, bool) {
	body := stripFences(raw)
	var imports, kept []string
	for _, line := range strings.Split(body, "\n") {
		if imp, isImp := importOf(line); isImp {
			if imp != "" && !contains(imports, imp) {
				imports = append(imports, imp)
			}
			continue
		}
		kept = append(kept, line)
	}
	body = strings.Join(kept, "\n")
	body = strings.TrimPrefix(body, "\n")
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		return InlineCompletion{}, false
	}
	// The model sometimes echoes the marker back.
	body = strings.ReplaceAll(body, CursorMarker, "")
	return InlineCompletion{InsertText: body, ImportLines: imports}, true
}

func importOf(line string) (string, bool) {
	t := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(t, "IMPORT:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(t, "IMPORT:")), true
}

func stripFences(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "```") {
		return raw
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	} else {
		s = ""
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return s
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ImportInsertion decides where the completion's imports go in a buffer:
// the 0-based line to insert at and the lines not already present. Go
// files get them right after the package clause; everything else at the
// top (below a shebang).
func ImportInsertion(lines []string, filetype string, imports []string) (int, []string) {
	have := map[string]bool{}
	for _, l := range lines {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, imp := range imports {
		imp = strings.TrimSpace(imp)
		if imp == "" || have[imp] {
			continue
		}
		// Go: `import "fmt"` is also satisfied by a `"fmt"` line inside an
		// import ( … ) block.
		if filetype == "go" && strings.HasPrefix(imp, "import ") && have[strings.TrimSpace(strings.TrimPrefix(imp, "import "))] {
			continue
		}
		add = append(add, imp)
	}
	if len(add) == 0 {
		return 0, nil
	}
	at := 0
	if filetype == "go" {
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "package ") {
				at = i + 1
				break
			}
		}
		return at, append([]string{""}, add...)
	}
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
		at = 1
	}
	return at, add
}
