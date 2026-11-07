package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// A shaped MCP result as a typed block (MOBUX-14 on MOB-3): the phone
// draws it with the same renderer as every other block. The block is
// made to fit the block caps (64 KiB in all): rows are dropped from the
// end until it does, and Truncated says so. The raw JSON travels beside
// the block (ViewData.Raw), not in it, so it never pushes rows out.

// Block turns d into a block with id, title and source ("mcp.<server>.
// <tool>"). It always returns a block that passes Validate: anything
// that does not fit becomes a short json or text block.
func (d ViewData) Block(id, title, source string) blocks.Block {
	b := blocks.Block{ID: blockID(id), Title: bs(orDefault(oneLine(title, blocks.MaxTitle), "MCP result"), blocks.MaxTitle),
		Source: bs(source, blocks.MaxTitle), Total: d.Total, Truncated: d.Truncated}
	switch d.Layout {
	case LayoutTable:
		b.Kind = blocks.KindTable
		for i, f := range d.Fields {
			if i == blocks.MaxColumns {
				break
			}
			b.Columns = append(b.Columns, blocks.BlockColumn{ID: fmt.Sprintf("c%d", i), Label: bs(f.Label, blocks.MaxTitle), Kind: colKind(f.Kind)})
		}
		if len(b.Columns) == 0 {
			return d.jsonBlock(b)
		}
		for _, r := range d.Rows {
			row := make([]string, len(b.Columns))
			for j := range row {
				if j < len(r) {
					row[j] = bs(r[j].S, blocks.MaxSub)
				}
			}
			b.Rows = append(b.Rows, row)
		}
	case LayoutList:
		b.Kind = blocks.KindList
		for i, r := range d.Rows {
			b.Items = append(b.Items, listItem(i, d.Fields, r))
		}
	case LayoutCard, LayoutDetail:
		b.Kind = blocks.KindDetail
		if d.Layout == LayoutCard {
			b.Kind = blocks.KindCard
		}
		if len(d.Rows) > 0 {
			for i, f := range d.Fields {
				if i == blocks.MaxFields || i >= len(d.Rows[0]) {
					break
				}
				b.Fields = append(b.Fields, blocks.BlockField{Label: bs(orDefault(f.Label, f.ID), blocks.MaxTitle),
					Value: bs(d.Rows[0][i].S, blocks.MaxSub), Kind: colKind(f.Kind)})
			}
		}
		if len(b.Fields) == 0 {
			return d.jsonBlock(b)
		}
	case LayoutStatus:
		b.Kind, b.State = blocks.KindStatus, blocks.ToneBad
		msg := oneLine(d.Text, blocks.MaxForYou)
		if msg == "" {
			msg = "The tool said it failed."
		}
		b.Items = []blocks.BlockItem{{ID: "error", Title: bs(msg, blocks.MaxTitle), Sub: bs(msg, blocks.MaxSub), Tone: blocks.ToneBad}}
	case LayoutText:
		b.Kind, b.MD = blocks.KindText, textMD(d.Text)
	default:
		return d.jsonBlock(b)
	}
	return fitBlock(b, d)
}

// fitBlock drops rows or items from the end until the block passes
// Validate; a block that still fails becomes json (or text).
func fitBlock(b blocks.Block, d ViewData) blocks.Block {
	for {
		err := b.Validate()
		if err == nil {
			return b
		}
		switch {
		case len(b.Rows) > 1:
			b.Rows, b.Truncated = b.Rows[:len(b.Rows)/2], true
		case len(b.Items) > 1:
			b.Items, b.Truncated = b.Items[:len(b.Items)/2], true
		case b.MD != nil && len(b.MD.C) > 1:
			b.MD.C, b.Truncated = b.MD.C[:len(b.MD.C)/2], true
		default:
			return d.jsonBlock(blocks.Block{ID: b.ID, Title: b.Title, Source: b.Source})
		}
		if b.Total == 0 {
			b.Total = len(d.Rows)
		}
	}
}

// jsonBlock is the raw data as a json block (valid JSON within the cap),
// else as text.
func (d ViewData) jsonBlock(b blocks.Block) blocks.Block {
	out := blocks.Block{ID: b.ID, Title: b.Title, Source: b.Source, Truncated: d.Truncated}
	raw := strings.TrimSpace(d.Raw)
	if raw != "" && len(raw) <= blocks.MaxJSON && json.Valid([]byte(raw)) {
		out.Kind, out.JSON = blocks.KindJSON, raw
		if out.Validate() == nil {
			return out
		}
	}
	text := raw
	if text == "" {
		text = d.Text
	}
	if text == "" {
		text = "(no data)"
	}
	if len(text) > blocks.MaxJSON {
		text, out.Truncated = clipText(text, blocks.MaxJSON), true
	}
	out.Kind, out.JSON, out.MD = blocks.KindText, "", &blocks.MdNode{K: "doc", C: []blocks.MdNode{{K: "codeblock", Text: text}}}
	if out.Validate() != nil {
		out.MD = &blocks.MdNode{K: "doc", C: []blocks.MdNode{{K: "p", C: []blocks.MdNode{{K: "text", Text: "The data is too big for the phone. Open it on the laptop."}}}}}
		out.Truncated = true
	}
	return out
}

// listItem is one row of a list: the first field is the title, the next
// two the sub line, the first time field the age.
func listItem(i int, fields []ViewField, r []Cell) blocks.BlockItem {
	it := blocks.BlockItem{ID: fmt.Sprintf("r%d", i)}
	var sub []string
	for j, c := range r {
		if j >= len(fields) {
			break
		}
		switch {
		case it.Title == "" && c.S != "":
			it.Title = bs(c.S, blocks.MaxTitle)
		case fields[j].Kind == FieldTime && it.Time == 0 && c.T > 0:
			it.Time = c.T
		case len(sub) < 2 && c.S != "":
			sub = append(sub, c.S)
		}
	}
	if it.Title == "" {
		it.Title = "(empty)"
	}
	it.Sub = bs(strings.Join(sub, " · "), blocks.MaxSub)
	return it
}

// bs is s as one block string: valid UTF-8, control characters (a
// newline too) as spaces, at most n runes.
func bs(s string, n int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return ' '
		}
		return r
	}, s)
	return clipRunes(s, n)
}

// textMD is plain text as paragraphs (one per blank-line block); the
// page draws every string as a text node.
func textMD(s string) *blocks.MdNode {
	doc := &blocks.MdNode{K: "doc"}
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		var c []blocks.MdNode
		for k, line := range strings.Split(para, "\n") {
			if k > 0 {
				c = append(c, blocks.MdNode{K: "br"})
			}
			c = append(c, blocks.MdNode{K: "text", Text: bs(line, MaxViewText)})
		}
		doc.C = append(doc.C, blocks.MdNode{K: "p", C: c})
	}
	if len(doc.C) == 0 {
		doc.C = []blocks.MdNode{{K: "p", C: []blocks.MdNode{{K: "text", Text: "(empty)"}}}}
	}
	return doc
}

func colKind(k string) string {
	switch k {
	case FieldNum:
		return blocks.ColNum
	case FieldTime:
		return blocks.ColTime
	case FieldJSON, FieldLink:
		return blocks.ColMono
	}
	return blocks.ColText
}

// blockID makes a valid block id of a view id ("mcp.s.@resources" has
// an "@", which a block id may not hold).
func blockID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:-", r)) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() == blocks.MaxID {
			break
		}
	}
	if b.Len() == 0 {
		return "mcp"
	}
	return b.String()
}
