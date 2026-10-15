package diffview

import (
	"fmt"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Commit mode: the card shows one git commit read-only (Source Control
// GRAPH → commit view). The model stays pure: the app loads file texts
// (LoadFileMsg → SetFileText) and runs the actions (ActionMsg).

// RefKind says what a ref on the commit is (same order as git.RefKind).
type RefKind uint8

const (
	RefLocal RefKind = iota
	RefRemote
	RefTag
)

// Ref is a branch or tag on the commit, drawn as a pill.
type Ref struct {
	Name string
	Kind RefKind
}

// Header is the commit shown above the diff.
type Header struct {
	Title            string // overrides "Commit <hash>" (compare: "Compare a → b")
	Hash, FullHash   string
	Subject, Body    string
	Author           string // name only: the e-mail is never on the card
	DateRel, DateAbs string
	Parents          []string // short hashes; each is a click target
	Refs             []Ref
	Files, Add, Del  int
	OldLabel         string // "− f31bb77 (parent)"
	NewLabel         string // "+ a1b2c3d (this commit)"
}

// Button is a footer button of the commit card.
type Button struct{ ID, Label, Kind string }

// ActionMsg is a commit action the app runs (copy-sha, open-file,
// open-at-commit, raw, revert, cherry-pick, parent:<hash>, more). File is
// the shown file.
type ActionMsg struct {
	ID   string
	File int
}

// LoadFileMsg asks the app for the text of file File (it is Pending).
type LoadFileMsg struct{ File int }

// commitKeys maps commit-mode keys to action ids (r is Raw, not reject).
var commitKeys = map[string]string{
	"r": "raw", "y": "copy-sha", "o": "open-file", "O": "open-at-commit",
	"v": "revert", "c": "cherry-pick", "b": "body",
}

// NewCommit builds a read-only card for one commit. Files with Pending
// set show Loading… until SetFileText.
func NewCommit(h Header, files []File, buttons []Button) Model {
	if len(files) == 0 {
		files = []File{{Note: "No file changes in this commit."}}
	}
	m := New("", files, false)
	m.header = &h
	m.buttons = buttons
	m.bodyOpen = true
	return m
}

// Header returns the commit header (ok false outside commit mode).
func (m Model) Header() (Header, bool) {
	if m.header == nil {
		return Header{}, false
	}
	return *m.header, true
}

// File returns file i as given (with any loaded text).
func (m Model) File(i int) File {
	if i < 0 || i >= len(m.files) {
		return File{}
	}
	return m.files[i].File
}

// SetFileText loads file i's two sides and computes its diff.
func (m *Model) SetFileText(i int, old, new string) {
	if i < 0 || i >= len(m.files) {
		return
	}
	f := newFileState(File{Name: m.files[i].Name, Tag: m.files[i].Tag, Status: m.files[i].Status,
		Add: m.files[i].Add, Del: m.files[i].Del, Old: old, New: new})
	m.files[i] = f
	m.cache.reset()
}

// SetFileNote shows note instead of file i's diff (and stops Loading…).
func (m *Model) SetFileNote(i int, note string) {
	if i < 0 || i >= len(m.files) {
		return
	}
	f := m.files[i]
	f.Note, f.Pending = note, false
	f.diff = textdiff.Compute("", "")
	m.cache.reset()
}

// SetBounds docks the card at screen rectangle (x, y, w, h); a zero
// width or height centres it on the screen again.
func (m *Model) SetBounds(x, y, w, h int) {
	m.bx, m.by, m.bw, m.bh = x, y, w, h
	m.cache.reset()
}

// ToggleBody folds or opens the commit message body.
func (m *Model) ToggleBody() { m.bodyOpen = !m.bodyOpen }

// loadCmd asks for the current file's text when it is not loaded yet.
func (m *Model) loadCmd() tea.Cmd {
	f := m.file()
	if m.header == nil || f == nil || !f.Pending {
		return nil
	}
	i := m.cur
	return func() tea.Msg { return LoadFileMsg{File: i} }
}

// ── Header layout ─────────────────────────────────────────────────────────

const maxBodyRows = 3

// bodyLines is the commit body wrapped to width.
func (h *Header) bodyLines(width int) []string {
	body := strings.TrimSpace(h.Body)
	if body == "" || width < 8 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(body, "\n") {
		out = append(out, wrapWords(strings.TrimRight(expandTabs(para), " "), width)...)
	}
	return out
}

// wrapWords breaks s into lines of at most width cells, at spaces when it
// can.
func wrapWords(s string, width int) []string {
	if runewidth.StringWidth(s) <= width {
		return []string{s}
	}
	var out []string
	line := ""
	for _, w := range strings.Split(s, " ") {
		for runewidth.StringWidth(w) > width {
			if line != "" {
				out = append(out, line)
				line = ""
			}
			head := runewidth.Truncate(w, width, "")
			out = append(out, head)
			w = w[len(head):]
		}
		switch {
		case line == "":
			line = w
		case runewidth.StringWidth(line)+1+runewidth.StringWidth(w) <= width:
			line += " " + w
		default:
			out = append(out, line)
			line = w
		}
	}
	return append(out, line)
}

// headerRows is the number of rows between the title and the labels:
// subject, body (open, tall cards only, at most 3) and the meta row.
func (m Model) headerRows(boxH, iw int) int {
	if m.header == nil {
		return 0
	}
	return 2 + len(m.shownBody(boxH, iw))
}

// shownBody is the body rows on show (the last one may be a "more" row).
func (m Model) shownBody(boxH, iw int) []string {
	if m.header == nil || !m.bodyOpen || boxH < 28 {
		return nil
	}
	lines := m.header.bodyLines(iw - 2)
	if len(lines) > maxBodyRows {
		more := len(lines) - (maxBodyRows - 1)
		lines = append(lines[:maxBodyRows-1:maxBodyRows-1], fmt.Sprintf("%s %d more lines · r raw", icons().fold, more))
	}
	return lines
}

// rowB builds one row of segments and records click targets.
type rowB struct {
	segs []seg
	hits []hit // x0 / x1 relative to the row start; y unused
}

func (r *rowB) add(text string, st lipgloss.Style, id string) {
	x := segsWidth(r.segs)
	r.segs = append(r.segs, seg{text, st})
	if id != "" {
		r.hits = append(r.hits, hit{x0: x, x1: x + runewidth.StringWidth(text), id: id})
	}
}

func (r *rowB) width() int { return segsWidth(r.segs) }

// pillSegs draws a ref as a pill (half-block caps in Nerd Font mode).
func pillSegs(r Ref, p pal) []seg {
	var fg, bg lipgloss.Color
	switch r.Kind {
	case RefTag:
		fg, bg = theme.TintPill(theme.AccentAmber, theme.BgFloat)
	case RefRemote:
		fg, bg = theme.LG(theme.TextMuted), theme.Wash(theme.BgFloat, theme.WashHover)
	default:
		fg, bg = theme.TintPill(theme.AccentBlue, theme.BgFloat)
	}
	name := fit(r.Name, 20)
	body := lipgloss.NewStyle().Background(bg).Foreground(fg)
	if theme.CurrentIconMode == theme.IconModeNerdFont {
		cap := lipgloss.NewStyle().Background(p.bg).Foreground(bg)
		return []seg{{"▐", cap}, {name, body}, {"▌", cap}}
	}
	return []seg{{" " + name + " ", body}}
}

// commitTitle is row 0 in commit mode: "⑂ Commit a1b2c3d · main" and the
// controls (change nav, Split / Unified, Raw, ✕).
func (m Model) commitTitle(g geom, f *fileState, p pal, ic glyphs) (left, right rowB) {
	h := m.header
	bg := lipgloss.NewStyle().Background(p.bg)
	left.add(" "+theme.GlyphMerge.String()+" ", bg.Foreground(p.accent), "")
	if h.Title != "" {
		left.add(h.Title, bg.Foreground(p.white).Bold(true), "")
	} else {
		left.add("Commit ", bg.Foreground(p.white).Bold(true), "")
		left.add(h.Hash, bg.Foreground(p.text), "copy-sha")
		for _, r := range h.Refs {
			if r.Kind == RefLocal {
				left.add("  ·  ", bg.Foreground(p.dim), "")
				left.add(r.Name, bg.Foreground(p.muted), "")
				break
			}
		}
	}
	n := len(f.diff.Hunks)
	pos := "No changes"
	if n > 0 {
		pos = fmt.Sprintf("Change %d of %d", f.focus+1, n)
	}
	btn := lipgloss.NewStyle().Background(p.sunken).Foreground(p.text)
	on := lipgloss.NewStyle().Background(p.sel).Foreground(p.white).Bold(true)
	off := lipgloss.NewStyle().Background(p.sunken).Foreground(p.dim)
	splitSt, uniSt := off, on
	if g.split {
		splitSt, uniSt = on, off
	}
	right.add(" "+ic.left+" ", btn, "prev")
	right.add(" "+pos+" ", bg.Foreground(p.muted), "")
	right.add(" "+ic.right+" ", btn, "next")
	right.add("  ", bg, "")
	right.add(" Split ", splitSt, "mode-split")
	right.add(" Unified ", uniSt, "mode-unified")
	if h.Hash != "" {
		right.add("  ", bg, "")
		right.add(" Raw ", btn, "raw")
	}
	right.add("  ", bg, "")
	right.add(" "+theme.GlyphClose.String()+" ", btn, "hide")
	right.add(" ", bg, "")
	if left.width()+right.width()+2 > g.iw && len(left.segs) > 3 {
		left.segs, left.hits = left.segs[:3], left.hits[:min(len(left.hits), 1)]
	}
	return left, right
}

// commitSubject is the subject row; a folded body shows "⋯ N lines".
func (m Model) commitSubject(g geom, p pal, ic glyphs) (left, right rowB) {
	h := m.header
	bg := lipgloss.NewStyle().Background(p.bg)
	left.add(" "+h.Subject, bg.Foreground(p.white), "")
	if len(m.shownBody(g.h, g.iw)) == 0 {
		if n := len(h.bodyLines(g.iw - 2)); n > 0 {
			word := "lines"
			if n == 1 {
				word = "line"
			}
			right.add(fmt.Sprintf("%s %d %s ", ic.fold, n, word), bg.Foreground(p.dim), "body")
		}
	}
	return left, right
}

// commitMeta is "Author · 2 hours ago · date · parent abc · pills" and the
// commit's totals on the right.
func (m Model) commitMeta(g geom, p pal) (left, right rowB) {
	h := m.header
	bg := lipgloss.NewStyle().Background(p.bg)
	dot := func(r *rowB) { r.add(" · ", bg.Foreground(p.dim), "") }
	files := "files"
	if h.Files == 1 {
		files = "file"
	}
	right.add(fmt.Sprintf("%d %s  ", h.Files, files), bg.Foreground(p.muted), "")
	right.add(fmt.Sprintf("+%d", h.Add), bg.Foreground(p.add), "")
	right.add(" ", bg, "")
	right.add(fmt.Sprintf("−%d", h.Del), bg.Foreground(p.del), "")
	right.add(" ", bg, "")
	build := func(abs bool, refs int) rowB {
		var l rowB
		l.add(" ", bg, "")
		if h.Author != "" {
			l.add(h.Author, bg.Foreground(p.text), "")
		}
		if h.DateRel != "" {
			dot(&l)
			l.add(h.DateRel, bg.Foreground(p.muted), "")
		}
		if abs && h.DateAbs != "" {
			dot(&l)
			l.add(h.DateAbs, bg.Foreground(p.dim), "")
		}
		if len(h.Parents) > 0 {
			dot(&l)
			word := "parent "
			if len(h.Parents) > 1 {
				word = "parents "
			}
			for i, par := range h.Parents {
				if i > 0 {
					l.add(", ", bg.Foreground(p.dim), "")
				}
				text := par
				if i == 0 {
					text = word + par
				}
				l.add(text, bg.Foreground(p.muted), "parent:"+par)
			}
		}
		for i, r := range h.Refs[:refs] {
			if i == 0 {
				dot(&l)
			} else {
				l.add(" ", bg, "")
			}
			l.segs = append(l.segs, pillSegs(r, p)...)
		}
		if refs < len(h.Refs) {
			l.add(fmt.Sprintf(" +%d", len(h.Refs)-refs), bg.Foreground(p.dim), "")
		}
		return l
	}
	room := g.iw - right.width() - 1
	for refs := len(h.Refs); refs >= 0; refs-- {
		if l := build(true, refs); l.width() <= room {
			return l, right
		}
	}
	for refs := len(h.Refs); refs >= 0; refs-- {
		if l := build(false, refs); l.width() <= room {
			return l, right
		}
	}
	return build(false, 0), right
}

// footerButtons splits the commit buttons into the shown ones and those
// folded into ⋯ when the row is too narrow. The last button (Close)
// always shows.
func (m Model) footerButtons(room int) (shown, folded []Button) {
	bw := func(b Button) int { return runewidth.StringWidth(b.Label) + 3 } // " label " + gap
	total := 0
	for _, b := range m.buttons {
		total += bw(b)
	}
	if total <= room || len(m.buttons) < 2 {
		return m.buttons, nil
	}
	last := m.buttons[len(m.buttons)-1]
	budget := room - bw(last) - (runewidth.StringWidth(icons().fold) + 3)
	for i, b := range m.buttons[:len(m.buttons)-1] {
		if folded == nil && bw(b) <= budget {
			shown = append(shown, b)
			budget -= bw(b)
			continue
		}
		folded = append(folded, m.buttons[i])
	}
	shown = append(shown, Button{ID: "more", Label: icons().fold})
	return append(shown, last), folded
}

// FoldedButtons are the footer buttons hidden behind ⋯ (for the app's
// menu).
func (m Model) FoldedButtons() []Button {
	g := m.geom()
	_, folded := m.footerButtons(g.iw - m.footerLeftW(g) - 1)
	return folded
}

// commitFooterLeft is the file name and its position in at most width
// cells (the name gives way first).
func (m Model) commitFooterLeft(f *fileState, width int) string {
	name := f.Name
	if f.Tag != "" {
		name += "  " + f.Tag
	}
	pos := ""
	if len(m.files) > 1 {
		pos = fmt.Sprintf(" · file %d of %d", m.cur+1, len(m.files))
	}
	return " " + fit(name, max(width-1-runewidth.StringWidth(pos), 4)) + pos
}

func (m Model) footerLeftW(g geom) int {
	f := m.file()
	if f == nil {
		return 0
	}
	return runewidth.StringWidth(m.commitFooterLeft(f, max(g.iw/2-2, 20)))
}

// commitListItem is one row of the commit file list:
// status · icon · name · dim dir · +a −d.
func commitListItem(i int, f *fileState, sel bool, w int, p pal) listItem {
	sbg := p.bg
	if sel {
		sbg = p.sel
	}
	st := lipgloss.NewStyle().Background(sbg)
	letter := string(rune(max(f.Status, ' ')))
	lc := p.mod
	switch f.Status {
	case 'A':
		lc = p.add
	case 'D':
		lc = p.del
	case 'R':
		lc = theme.LG(theme.AccentBlue)
	}
	icon, icol := theme.FileIconFor(f.Name, false, false)
	stats := fmt.Sprintf("+%d −%d ", f.Add, f.Del)
	if f.Binary {
		stats = "bin "
	}
	base, dir := path.Base(f.Name), path.Dir(f.Name)
	if dir == "." {
		dir = ""
	}
	lead := " " + letter + " "
	iconSeg := ""
	if icon != "" && runewidth.StringWidth(icon) == 1 {
		iconSeg = icon + " "
	}
	nameW := w - runewidth.StringWidth(lead) - runewidth.StringWidth(iconSeg) - runewidth.StringWidth(stats) - 1
	name := fit(base, max(nameW, 4))
	rest := max(nameW-runewidth.StringWidth(name), 0)
	dirText := ""
	if dir != "" && rest > 4 {
		dirText = fit(" "+dir, rest)
	}
	pad := max(w-runewidth.StringWidth(lead)-runewidth.StringWidth(iconSeg)-runewidth.StringWidth(name)-
		runewidth.StringWidth(dirText)-runewidth.StringWidth(stats), 1)
	return listItem{
		id:  fmt.Sprintf("file:%d", i),
		sel: sel,
		text: []seg{{lead, st.Foreground(lc)}, {iconSeg, st.Foreground(theme.LG(icol))}, {name, st.Foreground(p.text)},
			{dirText, st.Foreground(p.dim)}, {strings.Repeat(" ", pad), st}, {stats, st.Foreground(p.dim)}},
	}
}
