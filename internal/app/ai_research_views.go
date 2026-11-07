package app

import (
	"strings"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
)

// Research in chat (mobile plan, MOB-15). "Research: …" from the phone's
// Research chip is an AI turn on the laptop that uses the laptop's own
// web_search and web_fetch (off by default; the chip is off while both
// are, see AISnap.WebFetch / WebSearch). These views refine the tool
// views of MOB-11 for the phone:
//   - web_search → a sources list: title, site badge, snippet, and the
//     url as a ref of kind "url", so a tap opens it only after the page's
//     confirm sheet (a new tab, no opener, no referrer);
//   - web_fetch → a text block of the page (cut at 16 KiB) with "Open on
//     laptop".
// Both read only the text the model got: the search key never appears
// in it, and a page is data, never instructions.

// researchRefURL is the ref kind of a web address.
const researchRefURL = "url"

// researchMaxText is how much of a page's text the phone gets.
const researchMaxText = 16 << 10

// researchURLRef is the ref of an http(s) url, or nil when u is not one
// or is longer than a ref id (a cut url would open the wrong page).
func researchURLRef(u string) *blocks.BlockRef {
	u = strings.TrimSpace(u)
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return nil
	}
	if utf8.RuneCountInString(u) > blocks.MaxTitle || strings.ContainsAny(u, " \t\r\n\"<>") {
		return nil
	}
	return &blocks.BlockRef{Kind: researchRefURL, ID: u}
}

// researchSearchView is the sources list of web_search: MOB-11's list,
// each row with the url of its hit as a ref.
func researchSearchView(args map[string]any, out string) *blocks.Block {
	b := webSearchView(args, out)
	if b == nil || b.Kind != blocks.KindList {
		return b
	}
	urls := researchHitURLs(out)
	for i := range b.Items {
		if i < len(urls) {
			b.Items[i].Ref = researchURLRef(urls[i])
		}
	}
	b.Title = ai.ViewText("Sources · "+ai.ArgString(args, "query"), blocks.MaxTitle)
	return ai.ViewDone(b)
}

// researchHitURLs are the urls of the hits in web_search's text
// ("1. title\n   url\n   snippet"), one per hit, "" when a hit has none.
func researchHitURLs(out string) []string {
	var urls []string
	inHit, seen := false, false
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "   ") {
			if inHit && !seen {
				urls[len(urls)-1], seen = strings.TrimSpace(l), true
			}
			continue
		}
		num, _, ok := strings.Cut(l, ". ")
		if !ok || num == "" || strings.Trim(num, "0123456789") != "" {
			continue
		}
		urls = append(urls, "")
		inHit, seen = true, false
	}
	return urls
}

// researchFetchView is the text block of web_fetch. The text is
// "title\nurl (type[, cut])\n---\ntext" (aiWebFetchTool).
func researchFetchView(_ map[string]any, out string) *blocks.Block {
	head, body, ok := strings.Cut(out, "\n---\n")
	if !ok {
		return nil
	}
	hl := strings.Split(head, "\n")
	title, info := "", hl[len(hl)-1]
	if len(hl) > 1 {
		title = hl[0]
	}
	i := strings.LastIndex(info, " (")
	if i < 0 || !strings.HasSuffix(info, ")") {
		return nil
	}
	u := info[:i]
	cut := strings.HasSuffix(info, ", cut)")
	if title == "" {
		title = webHost(u)
	}
	if title == "" {
		title = "Web page"
	}
	if len(body) > researchMaxText {
		n := researchMaxText
		for n > 0 && !utf8.RuneStart(body[n]) {
			n--
		}
		body, cut = body[:n], true
	}
	doc := blocks.MdNode{K: "doc"}
	for _, para := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		if t := ai.ViewText(para, researchMaxText); t != "" {
			doc.C = append(doc.C, blocks.MdNode{K: "p", C: []blocks.MdNode{{K: "text", Text: t}}})
		}
	}
	if len(doc.C) == 0 {
		return nil
	}
	b := toolBlock(blocks.KindText, title)
	b.MD, b.Truncated = &doc, cut
	if ref := researchURLRef(u); ref != nil {
		b.Ref = ref
		b.Actions = []blocks.BlockAction{{ID: "laptop", Label: "Open on laptop", Kind: blocks.ActLaptop, Ref: ref}}
	}
	return ai.ViewDone(b)
}
