// Package help reads the user guide that docs/embed.go puts in the
// binary, so termocode can show it offline (over SSH, on Termux): it
// loads a page, turns the website-only syntax into plain Markdown
// (Prepare), lists the page's headings with the website's anchors
// (Slug), and resolves links between pages. It is pure: no UI, no disk,
// no network.
package help

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"

	"github.com/amin-jalali/termocode/docs"
)

// SiteBase is the website of the guide (site_url in mkdocs.yml).
const SiteBase = "https://amin-jalali.github.io/termocode/"

// Loc is a place in the guide: a page, an optional heading anchor and
// an optional text to bring into view below that heading.
type Loc struct {
	Page   string // "guides/ai-use.md"
	Anchor string // "chat-panel"; "" = top of the page
	Find   string // "`theme`"; "" = the heading itself
}

// String is the topic form Parse reads: "page#anchor?find".
func (l Loc) String() string {
	s := l.Page
	if l.Anchor != "" || l.Find != "" {
		s += "#" + l.Anchor
	}
	if l.Find != "" {
		s += "?" + l.Find
	}
	return s
}

// Parse reads a topic: "guides/ai-use.md#chat-panel", optionally with
// "?find text" after the anchor. The page must be a relative .md path
// that stays inside the guide.
func Parse(s string) (Loc, error) {
	var l Loc
	p, rest, _ := strings.Cut(s, "#")
	l.Anchor, l.Find, _ = strings.Cut(rest, "?")
	page, ok := cleanPage(p)
	if !ok {
		return Loc{}, fmt.Errorf("help: bad page %q", s)
	}
	l.Page = page
	return l, nil
}

// cleanPage cleans a page path; false when it is not a relative .md
// path inside the guide.
func cleanPage(p string) (string, bool) {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, ":") {
		return "", false
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") || !strings.HasSuffix(p, ".md") {
		return "", false
	}
	return p, true
}

// Heading is one heading of a page with its website anchor.
type Heading struct {
	Level int
	Text  string // plain text: code spans included, markup removed
	Slug  string // unique on the page, as on the website
}

// Page is one guide page, prepared for the terminal.
type Page struct {
	Path     string
	Title    string // the nav title, else the first heading
	Source   string // Prepare(raw)
	Headings []Heading
	starts   []int // byte offset of each heading in Source
}

// Section returns the source of the heading with this anchor, up to the
// next heading of the same or a higher level.
func (p *Page) Section(anchor string) (string, bool) {
	for i, h := range p.Headings {
		if h.Slug != anchor {
			continue
		}
		end := len(p.Source)
		for j := i + 1; j < len(p.Headings); j++ {
			if p.Headings[j].Level <= h.Level {
				end = p.starts[j]
				break
			}
		}
		return p.Source[p.starts[i]:end], true
	}
	return "", false
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*Page{}
)

// ErrNotEmbedded is returned for a page that is not in the built-in
// guide (an ADR, concepts/…): it is on the website only.
var ErrNotEmbedded = errors.New("not in the built-in guide")

// Has reports whether page is in the built-in guide.
func Has(page string) bool {
	p, ok := cleanPage(page)
	if !ok {
		return false
	}
	_, err := fs.Stat(docs.FS, p)
	return err == nil
}

// Load returns a page of the built-in guide, prepared and with its
// headings. Pages are parsed once and cached.
func Load(page string) (*Page, error) {
	p, ok := cleanPage(page)
	if !ok {
		return nil, fmt.Errorf("help: bad page %q", page)
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if pg := cache[p]; pg != nil {
		return pg, nil
	}
	raw, err := fs.ReadFile(docs.FS, p)
	if err != nil {
		return nil, fmt.Errorf("help: %s: %w", p, ErrNotEmbedded)
	}
	src := Prepare(string(raw))
	pg := &Page{Path: p, Source: src}
	pg.Headings, pg.starts = parseHeadings(src)
	pg.Title = navTitle(p)
	if pg.Title == "" && len(pg.Headings) > 0 {
		pg.Title = pg.Headings[0].Text
	}
	if pg.Title == "" {
		pg.Title = p
	}
	cache[p] = pg
	return pg, nil
}

// Resolve loads l's page and finds its anchor. With no anchor the
// heading is zero.
func Resolve(l Loc) (*Page, Heading, error) {
	pg, err := Load(l.Page)
	if err != nil {
		return nil, Heading{}, err
	}
	if l.Anchor == "" {
		return pg, Heading{}, nil
	}
	for _, h := range pg.Headings {
		if h.Slug == l.Anchor {
			return pg, h, nil
		}
	}
	return pg, Heading{}, fmt.Errorf("help: %s has no heading #%s", pg.Path, l.Anchor)
}

// Check validates one topic string: the page is embedded, the anchor
// exists, and the find text (if any) is in that section. Mapping tables
// in the app run every entry through it.
func Check(topic string) error {
	l, err := Parse(topic)
	if err != nil {
		return err
	}
	pg, _, err := Resolve(l)
	if err != nil {
		return err
	}
	if l.Find == "" {
		return nil
	}
	sec := pg.Source
	if l.Anchor != "" {
		sec, _ = pg.Section(l.Anchor)
	}
	if !strings.Contains(sec, l.Find) {
		return fmt.Errorf("help: %q is not in %s", l.Find, topic)
	}
	return nil
}

// CheckAll runs Check on every topic and returns one error per bad one.
func CheckAll(topics []string) []error {
	var errs []error
	for _, t := range topics {
		if err := Check(t); err != nil {
			errs = append(errs, fmt.Errorf("topic %q: %w", t, err))
		}
	}
	return errs
}

// SiteURL is the website address of l: "guides/ai-use.md#chat-panel" →
// SiteBase + "guides/ai-use/#chat-panel" (MkDocs directory URLs).
func SiteURL(l Loc) string {
	p := strings.TrimSuffix(l.Page, ".md")
	switch {
	case p == "index" || p == "README":
		p = ""
	case strings.HasSuffix(p, "/index") || strings.HasSuffix(p, "/README"):
		p = p[:strings.LastIndexByte(p, '/')+1]
	default:
		p += "/"
	}
	u := SiteBase + p
	if l.Anchor != "" {
		u += "#" + l.Anchor
	}
	return u
}

// RelLink resolves a link written on page from: "../reference/keys.md#file"
// from "guides/git.md" is reference/keys.md, anchor "file"; "#frag" is a
// heading on the same page. It returns false for web and mail links,
// other schemes, images and other files, absolute paths, and paths that
// leave the guide. The page may still be missing from the built-in guide
// (see Has).
func RelLink(from, dest string) (Loc, bool) {
	if dest == "" {
		return Loc{}, false
	}
	p, anchor, _ := strings.Cut(dest, "#")
	if p == "" {
		return Loc{Page: from, Anchor: anchor}, true
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return Loc{}, false
	}
	page, ok := cleanPage(path.Join(path.Dir(from), p))
	if !ok {
		return Loc{}, false
	}
	return Loc{Page: page, Anchor: anchor}, true
}

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// headings lists the headings of prepared Markdown with their anchors.
func headings(src string) []Heading {
	hs, _ := parseHeadings(src)
	return hs
}

func parseHeadings(src string) ([]Heading, []int) {
	source := []byte(src)
	doc := md.Parser().Parse(text.NewReader(source))
	var hs []Heading
	var starts []int
	var sl Slugger
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		t := strings.TrimSpace(PlainText(h, source))
		hs = append(hs, Heading{Level: h.Level, Text: t, Slug: sl.Next(t)})
		start := 0
		if l := h.Lines(); l.Len() > 0 {
			start = lineStart(source, l.At(0).Start)
		}
		starts = append(starts, start)
		return ast.WalkSkipChildren, nil
	})
	return hs, starts
}

func lineStart(src []byte, i int) int {
	for i > 0 && src[i-1] != '\n' {
		i--
	}
	return i
}

// PlainText is the text of n's inline children as the website's anchor
// sees it: text and code spans, no markup. internal/mdview uses the same
// rule for the heading marks it records.
func PlainText(n ast.Node, source []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch v := c.(type) {
			case *ast.Text:
				b.Write(v.Value(source))
				if v.SoftLineBreak() || v.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(v.Value)
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}
