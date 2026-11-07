package blocks

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// mapSkip are the keys whose values are names, not text: ids, kinds,
// tones, op names, paths and a spec. MapStrings leaves them as they are.
var mapSkip = setOf("id", "kind", "tone", "state", "op", "path", "old", "source", "ws", "spec", "view", "href", "lang", "k", "field", "density", "layout", "group")

// MapStrings returns a copy of b with f applied to every text the page
// shows (titles, subs, labels, values, cells, json, markdown text …);
// ids, kinds, tones, paths and the spec stay as they are. b is not
// changed. The laptop redacts a block with it before it leaves.
func (b *Block) MapStrings(f func(string) string) (*Block, error) {
	if b == nil {
		return nil, nil
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	tree = mapTree(tree, f)
	if raw, err = json.Marshal(tree); err != nil {
		return nil, err
	}
	out := new(Block)
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, err
	}
	return out, nil
}

func mapTree(v any, f func(string) string) any {
	switch t := v.(type) {
	case string:
		return f(t)
	case []any:
		for i := range t {
			t[i] = mapTree(t[i], f)
		}
	case map[string]any:
		for k, x := range t {
			if !mapSkip[k] {
				t[k] = mapTree(x, f)
			}
		}
	}
	return v
}

// PlainText is the block as short lines for the model (a block the user
// sends back, MOB-10): a head line with title, kind and source, then the
// items, rows, fields, bars or json, cut at max bytes (at a character)
// with a "… (cut)" mark. A nil block is "".
func (b *Block) PlainText(max int) string {
	if b == nil {
		return ""
	}
	var w strings.Builder
	w.WriteString(b.Title + " (" + b.Kind)
	if b.Source != "" {
		w.WriteString(", from " + b.Source)
	}
	w.WriteString(")\n")
	if b.ForYou != "" {
		w.WriteString(b.ForYou + "\n")
	}
	item := func(it BlockItem, indent string) {
		w.WriteString(indent + "- " + it.Title)
		if it.Sub != "" {
			w.WriteString(" — " + it.Sub)
		}
		if it.Badge != "" {
			w.WriteString(" [" + it.Badge + "]")
		}
		if it.Tone != "" {
			w.WriteString(" (" + it.Tone + ")")
		}
		w.WriteString("\n")
	}
	row := func(r []string, indent string) { w.WriteString(indent + strings.Join(r, " | ") + "\n") }
	if len(b.Columns) > 0 {
		labels := make([]string, len(b.Columns))
		for i, c := range b.Columns {
			labels[i] = c.Label
		}
		row(labels, "")
	}
	for _, r := range b.Rows {
		row(r, "")
	}
	for _, it := range b.Items {
		item(it, "")
	}
	for _, g := range b.Groups {
		fmt.Fprintf(&w, "%s (%d)\n", g.Label, g.Count)
		for _, it := range g.Items {
			item(it, "  ")
		}
		for _, r := range g.Rows {
			row(r, "  ")
		}
	}
	for _, f := range b.Fields {
		w.WriteString(f.Label + ": " + f.Value + "\n")
	}
	for _, x := range b.Bars {
		if x.Text != "" {
			w.WriteString(x.Label + ": " + x.Text + "\n")
		} else {
			fmt.Fprintf(&w, "%s: %v\n", x.Label, x.Value)
		}
	}
	if b.State != "" {
		w.WriteString("state: " + b.State + "\n")
	}
	if d := b.Diff; d != nil {
		fmt.Fprintf(&w, "diff %s +%d −%d\n", d.Path, d.Adds, d.Dels)
	}
	if bd := b.Board; bd != nil {
		for _, c := range bd.Columns {
			w.WriteString(c.Label + "\n")
			for _, it := range c.Items {
				item(it, "  ")
			}
		}
	}
	if b.MD != nil {
		mdText(&w, b.MD)
		w.WriteString("\n")
	}
	if b.JSON != "" {
		w.WriteString(b.JSON + "\n")
	}
	if b.Total > 0 && b.Truncated {
		fmt.Fprintf(&w, "(%d in all; only the first are shown)\n", b.Total)
	}
	s := strings.TrimRight(w.String(), "\n")
	if max > 0 && len(s) > max {
		i := max
		for i > 0 && !utf8.RuneStart(s[i]) {
			i--
		}
		s = s[:i] + "\n… (cut)"
	}
	return s
}

// mdText writes the text of a markdown AST, a line per block node.
func mdText(w *strings.Builder, n *MdNode) {
	w.WriteString(n.Text)
	for i := range n.C {
		mdText(w, &n.C[i])
	}
	for _, r := range n.Rows {
		for i := range r {
			mdText(w, &r[i])
			w.WriteString(" ")
		}
		w.WriteString("\n")
	}
	switch n.K {
	case "p", "h", "li", "codeblock", "blockquote", "br", "hr":
		w.WriteString("\n")
	}
}
