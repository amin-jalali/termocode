package ai

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Next edit suggestion: after the user changes a buffer, the model looks
// at the recent edits and the code around the caret and proposes ONE
// related change somewhere else in the same buffer (a renamed parameter →
// its uses below; a new field → the constructor). The answer is a tiny
// JSON object, checked strictly: anything odd means no suggestion.

// Limits of a next edit request and answer.
const (
	// NextEditHistory is how many recent edits the prompt shows.
	NextEditHistory = 3
	// NextEditWindow is how many lines above and below the caret the
	// model sees. Only lines it saw can be changed.
	NextEditWindow = 60
	// NextEditMaxDistance caps how far from the caret a suggestion may be.
	NextEditMaxDistance = 200
	// NextEditMaxLines caps the replaced lines and the new lines.
	NextEditMaxLines = 12
	// Buffers bigger than this are never sent.
	NextEditMaxBufLines = 5000
	NextEditMaxBufBytes = 512 * 1024
)

// EditHunk is one change of a buffer: at Row (0-based, in the new text)
// the lines Old became New.
type EditHunk struct {
	Row      int
	Old, New []string
}

// DiffHunk returns the one hunk that turns old into cur (common prefix
// and suffix cut off). ok=false: they are equal.
func DiffHunk(old, cur []string) (EditHunk, bool) {
	p := 0
	for p < len(old) && p < len(cur) && old[p] == cur[p] {
		p++
	}
	s := 0
	for s < len(old)-p && s < len(cur)-p && old[len(old)-1-s] == cur[len(cur)-1-s] {
		s++
	}
	o, n := old[p:len(old)-s], cur[p:len(cur)-s]
	if len(o) == 0 && len(n) == 0 {
		return EditHunk{}, false
	}
	return EditHunk{Row: p, Old: append([]string(nil), o...), New: append([]string(nil), n...)}, true
}

// EditHistory keeps the last NextEditHistory edits of one buffer.
type EditHistory struct{ edits []EditHunk }

// Add records an edit, dropping the oldest when full.
func (h *EditHistory) Add(e EditHunk) {
	h.edits = append(h.edits, e)
	if len(h.edits) > NextEditHistory {
		h.edits = append([]EditHunk(nil), h.edits[len(h.edits)-NextEditHistory:]...)
	}
}

// Edits returns the edits, oldest first.
func (h *EditHistory) Edits() []EditHunk { return h.edits }

// Last returns the newest edit.
func (h *EditHistory) Last() (EditHunk, bool) {
	if len(h.edits) == 0 {
		return EditHunk{}, false
	}
	return h.edits[len(h.edits)-1], true
}

// Reset forgets all edits.
func (h *EditHistory) Reset() { h.edits = nil }

// NextEditInput is what one request is built from. Lines is the whole
// buffer; only the window around CursorRow (0-based) goes into the prompt.
type NextEditInput struct {
	Filetype, Path string
	Edits          []EditHunk // oldest first; the last one is the edit just made
	Lines          []string
	CursorRow      int
}

// NextEditWindowRange is the 0-based [first, last) line range the model
// sees for a caret on row cursor in a buffer of total lines.
func NextEditWindowRange(total, cursor int) (first, last int) {
	first = max(cursor-NextEditWindow, 0)
	last = min(cursor+NextEditWindow+1, total)
	return first, max(last, first)
}

const nextEditSystemPrompt = "You predict the NEXT edit a programmer will make. You get their recent " +
	"edits (as -/+ lines) and part of the file with line numbers (\"12|code\"). " +
	"Find ONE place in the shown lines that must change because of the recent " +
	"edits (for example: a renamed name is still used with its old name; a new " +
	"field or parameter must be passed or set). Never change the lines that were " +
	"just edited. Answer with ONE JSON object and nothing else:\n" +
	"{\"start\": <first line number to replace>, \"end\": <last line number to replace>, " +
	"\"lines\": [<the new text of those lines, one string per line, with indentation>]}\n" +
	"If no edit is clearly needed, answer exactly {\"none\": true}."

// BuildNextEditPrompt returns the messages for one next edit request.
func BuildNextEditPrompt(in NextEditInput) []Message {
	var b strings.Builder
	name := baseName(in.Path)
	b.WriteString("File " + name)
	if in.Filetype != "" {
		b.WriteString(" (language: " + in.Filetype + ")")
	}
	b.WriteString("\n\nRecent edits (oldest first):\n")
	for _, e := range in.Edits {
		b.WriteString("Edit at line " + strconv.Itoa(e.Row+1) + ":\n")
		for _, l := range capLines(e.Old) {
			b.WriteString("-" + l + "\n")
		}
		for _, l := range capLines(e.New) {
			b.WriteString("+" + l + "\n")
		}
	}
	first, last := NextEditWindowRange(len(in.Lines), in.CursorRow)
	b.WriteString("\nCode (the caret is on line " + strconv.Itoa(in.CursorRow+1) + "):\n")
	for i := first; i < last; i++ {
		b.WriteString(strconv.Itoa(i+1) + "|" + in.Lines[i] + "\n")
	}
	return []Message{
		{Role: RoleSystem, Content: nextEditSystemPrompt},
		{Role: RoleUser, Content: b.String()},
	}
}

func capLines(ls []string) []string {
	if len(ls) > NextEditMaxLines {
		return ls[:NextEditMaxLines]
	}
	return ls
}

// NextEdit is a checked suggestion: replace the 0-based lines
// Start..End (inclusive) with Lines.
type NextEdit struct {
	Start, End int
	Lines      []string
}

type nextEditJSON struct {
	Start *int     `json:"start"`
	End   *int     `json:"end"`
	Lines []string `json:"lines"`
	None  bool     `json:"none"`
}

// ParseNextEdit reads and checks a model answer for the request built from
// in. ok=false (show nothing) unless it is one valid JSON object that:
// names lines the model saw, within NextEditMaxDistance of the caret, not
// the lines just edited, at most NextEditMaxLines each way, and really
// changes them.
func ParseNextEdit(raw string, in NextEditInput) (NextEdit, bool) {
	body := strings.TrimSpace(stripFences(raw))
	if body == "" || body[0] != '{' {
		return NextEdit{}, false
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(body)))
	dec.DisallowUnknownFields()
	var j nextEditJSON
	if dec.Decode(&j) != nil || dec.More() {
		return NextEdit{}, false
	}
	if _, err := dec.Token(); err == nil { // trailing junk after the object
		return NextEdit{}, false
	}
	if j.None || j.Start == nil || j.End == nil || j.Lines == nil {
		return NextEdit{}, false
	}
	ne := NextEdit{Start: *j.Start - 1, End: *j.End - 1, Lines: j.Lines}
	first, last := NextEditWindowRange(len(in.Lines), in.CursorRow)
	switch {
	case ne.Start < first || ne.End >= last || ne.Start > ne.End:
		return NextEdit{}, false
	case ne.End-ne.Start+1 > NextEditMaxLines || len(ne.Lines) > NextEditMaxLines:
		return NextEdit{}, false
	case absInt(ne.Start-in.CursorRow) > NextEditMaxDistance || absInt(ne.End-in.CursorRow) > NextEditMaxDistance:
		return NextEdit{}, false
	}
	for _, l := range ne.Lines {
		if strings.ContainsAny(l, "\n\r") || strings.Contains(l, CursorMarker) {
			return NextEdit{}, false
		}
	}
	if len(in.Edits) > 0 {
		e := in.Edits[len(in.Edits)-1]
		eEnd := e.Row + max(len(e.New), 1) - 1
		if ne.Start <= eEnd && ne.End >= e.Row {
			return NextEdit{}, false
		}
	}
	if equalLines(in.Lines[ne.Start:ne.End+1], ne.Lines) {
		return NextEdit{}, false
	}
	return ne, true
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// secretNameWords mark a file name that may hold secrets.
var secretNameWords = []string{"secret", "credential", "password", "passwd", "token", "private_key", "privatekey"}

// secretExts are file kinds that hold keys and certificates.
var secretExts = []string{".pem", ".key", ".p12", ".pfx", ".keystore", ".jks", ".asc", ".gpg", ".kdbx"}

// secretNames are exact file names that hold secrets.
var secretNames = map[string]bool{".netrc": true, ".npmrc": true, ".pypirc": true, ".pgpass": true,
	".htpasswd": true, ".git-credentials": true}

// NextEditAllowed reports whether a buffer may be sent for a next edit
// suggestion: a real file, not one whose name looks like it holds
// secrets, and not too big. why names the rule that said no.
func NextEditAllowed(path string, lines, size int) (ok bool, why string) {
	if path == "" {
		return false, "no file"
	}
	if IsSecretFileName(path) {
		return false, "secret file"
	}
	if lines > NextEditMaxBufLines || size > NextEditMaxBufBytes {
		return false, "file too big"
	}
	return true, ""
}

// IsSecretFileName reports whether path names a file that may hold
// secrets (.env files, keys, credentials): such files never leave the
// laptop for the AI or the web UI.
func IsSecretFileName(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	name := baseName(p)
	switch {
	case name == ".env" || strings.HasPrefix(name, ".env.") || strings.HasSuffix(name, ".env"):
		return true
	case secretNames[name] || strings.HasPrefix(name, "id_rsa") || strings.HasPrefix(name, "id_ed25519") ||
		strings.HasPrefix(name, "id_ecdsa") || strings.HasPrefix(name, "id_dsa"):
		return true
	case strings.Contains(p, "/.ssh/") || strings.Contains(p, "/.gnupg/") || strings.Contains(p, "/.aws/"):
		return true
	}
	for _, x := range secretExts {
		if strings.HasSuffix(name, x) {
			return true
		}
	}
	for _, w := range secretNameWords {
		if strings.Contains(name, w) {
			return true
		}
	}
	return false
}
