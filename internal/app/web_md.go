package app

import (
	"net/url"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Chat markdown for the page (WEB-13, web design 3.6). goldmark parses the
// text; the walk below keeps only the allowed node kinds (webproto.MdNode)
// and never produces HTML: raw HTML is dropped, a link keeps its href only
// for http and https, an image becomes its alt text, strikethrough and
// other extras become their text. The page creates elements from the
// kinds and text nodes from the strings; it has no markdown parser.

var webMdParser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough,
	extension.Linkify, extension.TaskList)).Parser()

// webMdCache keeps the ASTs of texts converted lately: a running chat
// asks for the same finished messages after every change.
var webMdCache = struct {
	sync.Mutex
	m map[string]*webproto.MdNode
}{m: map[string]*webproto.MdNode{}}

const webMdCacheMax = 256

// webMarkdown returns the AST of a markdown text (cleaned like all chat
// text). The result is shared: callers must not change it.
func webMarkdown(src string) *webproto.MdNode {
	webMdCache.Lock()
	n, ok := webMdCache.m[src]
	webMdCache.Unlock()
	if ok {
		return n
	}
	n = webMarkdownParse(src)
	webMdCache.Lock()
	if len(webMdCache.m) >= webMdCacheMax {
		clear(webMdCache.m)
	}
	webMdCache.m[src] = n
	webMdCache.Unlock()
	return n
}

func webMarkdownParse(src string) *webproto.MdNode {
	b := []byte(webChatText(src))
	doc := webMdParser.Parse(text.NewReader(b), parser.WithContext(parser.NewContext()))
	w := &webMdWalker{src: b}
	root := webproto.MdNode{K: webproto.MdDoc, C: w.children(doc, 1)}
	return &root
}

type webMdWalker struct {
	src   []byte
	nodes int
}

// children converts the children of n at depth d.
func (w *webMdWalker) children(n gast.Node, d int) []webproto.MdNode {
	var out []webproto.MdNode
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if w.nodes >= webproto.MdMaxNodes {
			out = webMdAppendText(out, "…")
			break
		}
		out = w.node(out, c, d)
	}
	return out
}

// node appends the conversion of n to out (a node may become several, or
// none).
func (w *webMdWalker) node(out []webproto.MdNode, n gast.Node, d int) []webproto.MdNode {
	if d > webproto.MdMaxDepth { // too deep: its words, flat
		return webMdAppendText(out, w.plain(n))
	}
	w.nodes++
	add := func(k string) []webproto.MdNode {
		return append(out, webproto.MdNode{K: k, C: w.children(n, d+1)})
	}
	switch n := n.(type) {
	case *gast.Paragraph:
		return add(webproto.MdParagraph)
	case *gast.TextBlock: // a tight list item's text: inline in the item
		return webMdAppendNodes(out, w.children(n, d))
	case *gast.Heading:
		return append(out, webproto.MdNode{K: webproto.MdHeading, Level: min(max(n.Level, 1), 6), C: w.children(n, d+1)})
	case *gast.ThematicBreak:
		return append(out, webproto.MdNode{K: webproto.MdRule})
	case *gast.CodeBlock:
		return append(out, webproto.MdNode{K: webproto.MdCodeBlock, Text: w.lines(n)})
	case *gast.FencedCodeBlock:
		return append(out, webproto.MdNode{K: webproto.MdCodeBlock, Lang: webMdLang(string(n.Language(w.src))), Text: w.lines(n)})
	case *gast.Blockquote:
		return add(webproto.MdQuote)
	case *gast.List:
		if !n.IsOrdered() {
			return add(webproto.MdList)
		}
		start := n.Start
		if start == 1 || start < 0 {
			start = 0
		}
		return append(out, webproto.MdNode{K: webproto.MdOrdered, Start: start, C: w.children(n, d+1)})
	case *gast.ListItem:
		return add(webproto.MdItem)
	case *gast.HTMLBlock, *gast.RawHTML:
		return out // never: no HTML reaches the page, not even as text
	case *gast.Text:
		v := n.Segment.Value(w.src)
		if !n.IsRaw() {
			v = webMdUnescape(v)
		}
		out = webMdAppendText(out, string(v))
		switch {
		case n.HardLineBreak():
			out = append(out, webproto.MdNode{K: webproto.MdBreak})
		case n.SoftLineBreak():
			out = webMdAppendText(out, "\n")
		}
		return out
	case *gast.String:
		v := n.Value
		if !n.IsRaw() && !n.IsCode() {
			v = webMdUnescape(v)
		}
		return webMdAppendText(out, string(v))
	case *gast.CodeSpan:
		return append(out, webproto.MdNode{K: webproto.MdCode, Text: w.plain(n)})
	case *gast.Emphasis:
		if n.Level >= 2 {
			return add(webproto.MdStrong)
		}
		return add(webproto.MdEm)
	case *gast.Link:
		if href, ok := webMdHref(string(n.Destination)); ok {
			return append(out, webproto.MdNode{K: webproto.MdLink, Href: href, C: w.children(n, d+1)})
		}
		return webMdAppendNodes(out, w.children(n, d)) // not http(s): the words only
	case *gast.AutoLink:
		label := string(n.Label(w.src))
		if href, ok := webMdHref(string(n.URL(w.src))); ok && n.AutoLinkType == gast.AutoLinkURL {
			return append(out, webproto.MdNode{K: webproto.MdLink, Href: href, C: []webproto.MdNode{{K: webproto.MdText, Text: label}}})
		}
		return webMdAppendText(out, label)
	case *gast.Image: // no images from chat text: the alt words
		return webMdAppendText(out, w.plain(n))
	case *east.TaskCheckBox:
		if n.IsChecked {
			return webMdAppendText(out, "☑ ")
		}
		return webMdAppendText(out, "☐ ")
	case *east.Table:
		return append(out, w.table(n, d))
	}
	// Strikethrough and anything else: its content, without the wrapper.
	return webMdAppendNodes(out, w.children(n, d))
}

// table converts a GFM table: the header row's cells are th, the rest td.
func (w *webMdWalker) table(t *east.Table, d int) webproto.MdNode {
	out := webproto.MdNode{K: webproto.MdTable}
	for r := t.FirstChild(); r != nil; r = r.NextSibling() {
		kind := webproto.MdCell
		if _, head := r.(*east.TableHeader); head {
			kind = webproto.MdHeadCell
		}
		var row []webproto.MdNode
		for c := r.FirstChild(); c != nil; c = c.NextSibling() {
			w.nodes++
			row = append(row, webproto.MdNode{K: kind, C: w.children(c, d+2)})
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

// lines is a code block's text.
func (w *webMdWalker) lines(n gast.Node) string {
	var b strings.Builder
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		s := l.At(i)
		b.Write(s.Value(w.src))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// plain is the text of n's subtree, without markup.
func (w *webMdWalker) plain(n gast.Node) string {
	var b strings.Builder
	_ = gast.Walk(n, func(c gast.Node, entering bool) (gast.WalkStatus, error) {
		if !entering {
			return gast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *gast.Text:
			b.Write(webMdUnescape(c.Segment.Value(w.src)))
			if c.SoftLineBreak() || c.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *gast.String:
			b.Write(c.Value)
		case *gast.RawHTML, *gast.HTMLBlock:
			return gast.WalkSkipChildren, nil
		}
		return gast.WalkContinue, nil
	})
	return b.String()
}

// webMdAppendText adds text to out, joined with a text node before it.
func webMdAppendText(out []webproto.MdNode, s string) []webproto.MdNode {
	if s == "" {
		return out
	}
	if n := len(out); n > 0 && out[n-1].K == webproto.MdText {
		out[n-1].Text += s
		return out
	}
	return append(out, webproto.MdNode{K: webproto.MdText, Text: s})
}

// webMdAppendNodes adds nodes to out, joining text nodes that meet.
func webMdAppendNodes(out, nodes []webproto.MdNode) []webproto.MdNode {
	for i, n := range nodes {
		if n.K == webproto.MdText {
			out = webMdAppendText(out, n.Text)
			continue
		}
		return append(out, nodes[i:]...)
	}
	return out
}

func webMdUnescape(v []byte) []byte {
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
}

// webMdHref keeps a link target only when it is an absolute http or https
// URL with a host; anything else (javascript:, data:, a path) is no link.
func webMdHref(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, "\x00\r\n\t \\") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.String(), true
	}
	return "", false
}

// webMdLang keeps a code block's info word when it looks like a language
// name.
func webMdLang(s string) string {
	if len(s) > webproto.MdMaxLangLen {
		return ""
	}
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("+-_.#", r)
		if !ok {
			return ""
		}
	}
	return s
}

// webChatText cleans chat text for the page: webClean (controls) and no
// bidi embeddings, overrides or isolates, so a message cannot reorder the
// page around it. Newlines and tabs stay.
func webChatText(s string) string {
	s = webClean(s)
	if !strings.ContainsAny(s, "‪‫‬‭‮⁦⁧⁨⁩") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			return -1
		}
		return r
	}, s)
}
