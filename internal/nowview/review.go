package nowview

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Review is one pull request as the Review page shows it.
type Review struct {
	Number              int
	Title, Author, Head string
	Files, Add, Del     int
	I, N                int // "Review I of N"
	Risk                string
	Reasons             []string
	Question, Task      string
	Solution            string
	Groups              []Group
	Proof               []Proof
	Suggestion          string
	Notes               int
	URL                 string
}

// Group is one group of changes.
type Group struct {
	Title, Detail string
	Add, Del      int
	Main          bool
}

// Proof is one proof row: State ok, fail, running, stale or notrun.
type Proof struct {
	Name, State, SHA string
	Fix, Again       bool
}

// ReviewPage is the Review page.
type ReviewPage struct {
	r      Review
	w, h   int
	asking bool
	input  string
	note   string
	undo   bool
}

// NewReview makes the page for r.
func NewReview(r Review) *ReviewPage { return &ReviewPage{r: r} }

// SetReview shows r.
func (p *ReviewPage) SetReview(r Review) { p.r = r }

// Number is the pull request on show.
func (p *ReviewPage) Number() int { return p.r.Number }

// SetSize sets the page's size.
func (p *ReviewPage) SetSize(w, h int) { p.w, p.h = w, h }

// Note shows a line above the keys (undo: with "u undo").
func (p *ReviewPage) Note(s string, undo bool) { p.note, p.undo = s, undo }

func (p *ReviewPage) first(state string) (Proof, bool) {
	for _, x := range p.r.Proof {
		if x.State == state {
			return x, true
		}
	}
	return Proof{}, false
}

// Key reads one key.
func (p *ReviewPage) Key(k tea.KeyMsg) Action {
	if p.asking {
		switch k.Type {
		case tea.KeyEsc:
			p.asking = false
		case tea.KeyEnter:
			p.asking = false
			if t := strings.TrimSpace(p.input); t != "" {
				return Action{Do: "comment", Number: p.r.Number, Head: p.r.Head, Text: t}
			}
		case tea.KeyBackspace:
			if r := []rune(p.input); len(r) > 0 {
				p.input = string(r[:len(r)-1])
			}
		case tea.KeySpace:
			p.input += " "
		case tea.KeyRunes:
			if len(p.input) < 2000 {
				p.input += string(k.Runes)
			}
		}
		return Action{}
	}
	switch k.String() {
	case "esc", "q":
		return Action{Do: "close"}
	case "a":
		return Action{Do: "approve", Number: p.r.Number, Head: p.r.Head}
	case "s":
		return Action{Do: "skip", Number: p.r.Number}
	case "o":
		return Action{Do: "openfull", Number: p.r.Number, Text: p.r.URL}
	case "c":
		p.asking, p.input = true, ""
	case "u", "z":
		return Action{Do: "undo"}
	case "f":
		if x, ok := p.first("fail"); ok {
			return Action{Do: "fix", Number: p.r.Number, Text: x.Name}
		}
	case "g":
		if x, ok := p.first("stale"); ok {
			return Action{Do: "again", Number: p.r.Number, Text: x.Name}
		}
	case "l":
		if x, ok := p.first("fail"); ok {
			return Action{Do: "log", Number: p.r.Number, Text: x.Name}
		}
	}
	return Action{}
}

// View draws the page: exactly h lines of w cells.
func (p *ReviewPage) View() string {
	c := colors()
	bg := lipgloss.NewStyle().Background(c.bg).Foreground(c.text)
	bar := lipgloss.NewStyle().Background(c.bar).Foreground(c.onBar)
	muted := bg.Foreground(c.muted)
	w := max(p.w, 20)
	r := p.r
	var out []string
	add := func(s string, st lipgloss.Style) { out = append(out, line(s, w, st)) }
	add(" Review "+strconv.Itoa(r.I)+" of "+strconv.Itoa(r.N)+" · #"+strconv.Itoa(r.Number)+" "+r.Title, bar)
	add(" "+strings.ToUpper(r.Risk)+" RISK  "+r.Author+" · "+strconv.Itoa(r.Files)+" files +"+strconv.Itoa(r.Add)+" -"+strconv.Itoa(r.Del)+" · commit "+r.Head, bg.Foreground(c.warn))
	add("", bg)
	if r.Question != "" {
		q := r.Question
		if r.Task != "" && !strings.Contains(q, r.Task) {
			q += "  " + r.Task
		}
		add(" QUESTION  "+q, bg)
	}
	add(" SOLUTION  "+r.Solution, bg)
	add("", bg)
	add(" CHANGES · main first", muted)
	for i, g := range r.Groups {
		t := "   " + strconv.Itoa(i+1) + " " + cell(g.Title, 28) + "  " + cell(g.Detail, 22)
		if g.Main {
			t += "  +" + strconv.Itoa(g.Add) + " -" + strconv.Itoa(g.Del)
			add(t, bg.Bold(true))
		} else {
			add(t+"  ‣ (enter)", muted)
		}
	}
	add("", bg)
	add(" PROOF · "+r.Head, muted)
	for _, x := range r.Proof {
		mark, st, tail := "✓", bg.Foreground(c.ok), x.SHA
		switch x.State {
		case "fail":
			mark, st, tail = "×", bg.Foreground(c.bad), x.SHA+"          f ✦ fix   l log"
		case "stale":
			mark, st, tail = "◷", bg.Foreground(c.warn), "old code "+x.SHA+"   g run again"
		case "running":
			mark, st, tail = "◷", muted, "running"
		case "notrun":
			mark, st, tail = "–", muted, "not run"
		}
		add("   "+mark+" "+cell(x.Name, 20)+"  "+tail, st)
	}
	add("", bg)
	add(" RISKS      "+strings.Join(r.Reasons, " · "), bg)
	notes := ""
	if r.Notes > 0 {
		notes = strconv.Itoa(r.Notes) + " note "
	}
	add(" SUGGESTION "+notes+r.Suggestion+"   (on "+r.Head+")", bg)
	var foot []string
	if p.asking {
		foot = append(foot, line(" Comment on #"+strconv.Itoa(r.Number)+": "+p.input+"▏   enter send & next · esc cancel", w, bg.Bold(true)))
	}
	if p.note != "" {
		n := " " + p.note
		if p.undo {
			n += "   u undo"
		}
		foot = append(foot, line(n, w, bg.Foreground(c.ok)))
	}
	keys := " o open full   s skip   c comment & next   a approve & next   approve ≠ merge · posts APPROVE on " + r.Head
	foot = append(foot, line(keys, w, bar))
	h := max(p.h, len(foot)+1)
	for len(out) < h-len(foot) {
		add("", bg)
	}
	out = append(out[:h-len(foot)], foot...)
	return strings.Join(out, "\n")
}
