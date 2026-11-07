// Package canvas is the workspace's shared plan (CMP-92): one board of
// cards that the user and the AI both edit, reorder and mark. The user
// approves a card; the AI can propose, edit and move cards and mark
// their status, but never approves one and never removes a card the
// user wrote or approved. The board lives in .termocode/canvas.json of
// the workspace (plain JSON, so it travels with the project and is easy
// to read in a diff).
package canvas

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Statuses of a card.
const (
	StatusTodo  = "todo"
	StatusDoing = "doing"
	StatusDone  = "done"
)

// Statuses lists them in board order.
var Statuses = []string{StatusTodo, StatusDoing, StatusDone}

// Who changed the board.
const (
	ByYou = "you"
	ByAI  = "ai"
)

// Op kinds.
const (
	OpAdd       = "add"
	OpEdit      = "edit"
	OpStatus    = "status"
	OpMove      = "move"
	OpApprove   = "approve"
	OpUnapprove = "unapprove"
	OpRemove    = "remove"
)

// OpKinds lists the op kinds (the AI tool's enum: approve and unapprove
// are the user's only, so the tool leaves them out).
var OpKinds = []string{OpAdd, OpEdit, OpStatus, OpMove, OpRemove}

// Limits keep the file small and every row one line.
const (
	MaxCards = 200
	MaxTitle = 120  // runes
	MaxNote  = 1000 // runes
	MaxFiles = 10
	MaxFile  = 200 // runes of one file path
	FileName = "canvas.json"
)

// Card is one item of the plan.
type Card struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Note     string   `json:"note,omitempty"`
	Status   string   `json:"status"`
	By       string   `json:"by"` // who wrote it: you or ai
	Approved bool     `json:"approved,omitempty"`
	Files    []string `json:"files,omitempty"` // workspace-relative paths it is about
	// UpdatedMs is the last change (Unix ms) and UpdatedBy who made it.
	UpdatedMs int64  `json:"updated_ms,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// NeedsOK reports whether the card waits for the user's approval.
func (c Card) NeedsOK() bool { return !c.Approved }

// Board is the whole plan, cards in order.
type Board struct {
	Version int    `json:"version"` // goes up on every save
	Cards   []Card `json:"cards"`
}

// Path is the board file of the workspace at root.
func Path(root string) string { return filepath.Join(root, ".termocode", FileName) }

// Load reads the board of root; a missing file is an empty board.
func Load(root string) (Board, error) {
	data, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return Board{}, nil
	}
	if err != nil {
		return Board{}, err
	}
	var b Board
	if err := json.Unmarshal(data, &b); err != nil {
		return Board{}, fmt.Errorf("%s is not a valid plan: %w", filepath.Join(".termocode", FileName), err)
	}
	if len(b.Cards) > MaxCards {
		b.Cards = b.Cards[:MaxCards]
	}
	for i := range b.Cards {
		c := &b.Cards[i]
		if !validStatus(c.Status) {
			c.Status = StatusTodo
		}
		if c.By != ByAI {
			c.By = ByYou
		}
	}
	return b, nil
}

// Save writes b to root (a temp file, then a rename) and bumps Version.
func Save(root string, b *Board) error {
	p := Path(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b.Version++
	if b.Cards == nil {
		b.Cards = []Card{}
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".canvas-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Op is one change to the board.
type Op struct {
	Kind   string
	ID     string // every kind but add
	Title  string // add (required), edit
	Note   string // add, edit (with SetNote)
	Status string // add, status
	To     int    // move: the new 1-based place (1 = top)
	Files  []string
	// SetNote / SetFiles: edit changes the note / the files (an empty
	// value clears them).
	SetNote, SetFiles bool
}

// Clean makes text safe to store and draw: Redact (may be nil) first,
// then control characters out; a title is one line.
type Clean func(string) string

// Apply runs op on b for by (ByYou or ByAI) at now and returns the id of
// the card it touched and a short line of what it did.
func (b *Board) Apply(op Op, by string, now time.Time, redact Clean) (string, string, error) {
	if by != ByAI {
		by = ByYou
	}
	if redact == nil {
		redact = func(s string) string { return s }
	}
	title := cut(oneLine(redact(op.Title)), MaxTitle)
	note := cut(multiLine(redact(op.Note)), MaxNote)
	files, err := cleanFiles(op.Files)
	if err != nil {
		return "", "", err
	}
	stamp := func(c *Card) { c.UpdatedMs, c.UpdatedBy = now.UnixMilli(), by }
	if op.Kind == OpAdd {
		if title == "" {
			return "", "", errors.New("a card needs a title")
		}
		if len(b.Cards) >= MaxCards {
			return "", "", fmt.Errorf("the plan has %d cards, the most it may have", MaxCards)
		}
		st := op.Status
		if st == "" {
			st = StatusTodo
		}
		if !validStatus(st) {
			return "", "", badStatus(st)
		}
		c := Card{ID: b.nextID(), Title: title, Note: note, Status: st, By: by, Approved: by == ByYou, Files: files}
		stamp(&c)
		b.Cards = append(b.Cards, c)
		return c.ID, "added " + c.ID + " " + quote(c.Title), nil
	}
	i := b.Find(op.ID)
	if i < 0 {
		return "", "", fmt.Errorf("no card %q in the plan", op.ID)
	}
	c := &b.Cards[i]
	switch op.Kind {
	case OpEdit:
		changed := false
		if title != "" && title != c.Title {
			c.Title, changed = title, true
		}
		if op.SetNote && note != c.Note {
			c.Note, changed = note, true
		}
		if op.SetFiles {
			c.Files, changed = files, true
		}
		if !changed {
			return c.ID, "no change to " + c.ID, nil
		}
		stamp(c)
		if by == ByAI && c.Approved {
			c.Approved = false
			return c.ID, "edited " + c.ID + " (it needs the user's OK again)", nil
		}
		return c.ID, "edited " + c.ID, nil
	case OpStatus:
		if !validStatus(op.Status) {
			return "", "", badStatus(op.Status)
		}
		c.Status = op.Status
		stamp(c)
		return c.ID, c.ID + " is " + StatusWord(c.Status) + " now", nil
	case OpMove:
		to := min(max(op.To, 1), len(b.Cards)) - 1
		card := *c
		b.Cards = append(b.Cards[:i], b.Cards[i+1:]...)
		b.Cards = append(b.Cards[:to], append([]Card{card}, b.Cards[to:]...)...)
		stamp(&b.Cards[to])
		return card.ID, "moved " + card.ID + " to place " + strconv.Itoa(to+1), nil
	case OpApprove, OpUnapprove:
		if by == ByAI {
			return "", "", errors.New("only the user approves cards")
		}
		c.Approved = op.Kind == OpApprove
		stamp(c)
		if c.Approved {
			return c.ID, "approved " + c.ID, nil
		}
		return c.ID, c.ID + " needs an OK again", nil
	case OpRemove:
		if by == ByAI && (c.By != ByAI || c.Approved) {
			return "", "", fmt.Errorf("card %s was written or approved by the user: only the user removes it (mark it done, or edit its note)", c.ID)
		}
		id := c.ID
		b.Cards = append(b.Cards[:i], b.Cards[i+1:]...)
		return id, "removed " + id, nil
	}
	return "", "", fmt.Errorf("unknown change %q (one of %s)", op.Kind, strings.Join(OpKinds, ", "))
}

// Find is the index of the card id (-1: none).
func (b Board) Find(id string) int {
	for i, c := range b.Cards {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// Counts are the cards per status and how many wait for an OK.
func (b Board) Counts() (todo, doing, done, needOK int) {
	for _, c := range b.Cards {
		switch c.Status {
		case StatusDoing:
			doing++
		case StatusDone:
			done++
		default:
			todo++
		}
		if c.NeedsOK() {
			needOK++
		}
	}
	return
}

// StatusWord is a status for people: "to do", "doing", "done".
func StatusWord(s string) string {
	if s == StatusTodo {
		return "to do"
	}
	return s
}

// Text is the board for the AI (canvas_read): one line per card in
// order, then its note and files indented.
func (b Board) Text() string {
	if len(b.Cards) == 0 {
		return "The shared plan is empty."
	}
	todo, doing, done, need := b.Counts()
	var s strings.Builder
	fmt.Fprintf(&s, "Shared plan: %d to do, %d doing, %d done; %d wait for the user's OK.\n", todo, doing, done, need)
	for n, c := range b.Cards {
		ok := "approved"
		if c.NeedsOK() {
			ok = "needs the user's OK"
		}
		fmt.Fprintf(&s, "%d. %s [%s] %s (by %s, %s)\n", n+1, c.ID, StatusWord(c.Status), c.Title, byWord(c.By), ok)
		if c.Note != "" {
			for _, l := range strings.Split(c.Note, "\n") {
				s.WriteString("   " + l + "\n")
			}
		}
		if len(c.Files) > 0 {
			s.WriteString("   files: " + strings.Join(c.Files, ", ") + "\n")
		}
	}
	return s.String()
}

// Markdown is the board as a Markdown checklist (Copy as Markdown).
func (b Board) Markdown() string {
	var s strings.Builder
	s.WriteString("# Shared plan\n\n")
	if len(b.Cards) == 0 {
		s.WriteString("(empty)\n")
		return s.String()
	}
	for _, c := range b.Cards {
		box := "[ ]"
		if c.Status == StatusDone {
			box = "[x]"
		}
		var tags []string
		if c.Status == StatusDoing {
			tags = append(tags, "doing")
		}
		if c.By == ByAI {
			tags = append(tags, "by AI")
		}
		if c.NeedsOK() {
			tags = append(tags, "needs OK")
		}
		line := "- " + box + " " + c.Title
		if len(tags) > 0 {
			line += " _(" + strings.Join(tags, ", ") + ")_"
		}
		s.WriteString(line + "\n")
		if c.Note != "" {
			for _, l := range strings.Split(c.Note, "\n") {
				s.WriteString("  " + l + "\n")
			}
		}
		for _, f := range c.Files {
			s.WriteString("  - `" + f + "`\n")
		}
	}
	return s.String()
}

func byWord(by string) string {
	if by == ByAI {
		return "AI"
	}
	return "the user"
}

// nextID is "c" and one more than the highest number in use.
func (b Board) nextID() string {
	n := 0
	for _, c := range b.Cards {
		if v, err := strconv.Atoi(strings.TrimPrefix(c.ID, "c")); err == nil && strings.HasPrefix(c.ID, "c") && v > n {
			n = v
		}
	}
	return "c" + strconv.Itoa(n+1)
}

func validStatus(s string) bool { return s == StatusTodo || s == StatusDoing || s == StatusDone }

func badStatus(s string) error {
	return fmt.Errorf("status must be one of %s (got %q)", strings.Join(Statuses, ", "), s)
}

func quote(s string) string { return strconv.Quote(s) }

// oneLine is s with control characters and line breaks as spaces, runs
// of space as one.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)), " ")
}

// multiLine keeps line breaks (at most 20 lines) and drops the other
// control characters.
func multiLine(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	for i, l := range lines {
		lines[i] = oneLine(l)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// cleanFiles keeps workspace-relative paths only (no "..", not absolute).
func cleanFiles(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, f := range in {
		f = oneLine(f)
		if f == "" {
			continue
		}
		f = filepath.ToSlash(filepath.Clean(f))
		if filepath.IsAbs(f) || strings.HasPrefix(f, "/") || f == ".." || strings.HasPrefix(f, "../") {
			return nil, fmt.Errorf("file %q must be a path inside the workspace", f)
		}
		if utf8.RuneCountInString(f) > MaxFile || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
		if len(out) == MaxFiles {
			break
		}
	}
	return out, nil
}
