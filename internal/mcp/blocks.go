package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
)

// MCP results as phone blocks (MOB-12, mobile plan 3.4). When the AI
// calls an MCP tool, the phone shows the result as a typed block instead
// of raw text: an array of objects is a table, an array of plain values a
// list, a flat object a detail, links and media a list (media stay on
// the laptop), text a text block, isError a red status. Every string is
// cleaned and redacted here on the laptop; a result that does not fit a
// block becomes a short "open on the laptop" text. BlockOf always
// returns a block that passes blocks.Validate.

// BlockOptions tune BlockOf.
type BlockOptions struct {
	// ID is the block id ("" → "mcp").
	ID string
	// Clean strips escape sequences and control runes (nil: none).
	Clean func(string) string
	// Markdown builds a text block's tree (nil: plain paragraphs).
	Markdown func(string) *blocks.MdNode
}

const (
	maxBlockText = 8 << 10 // bytes of text drawn in a text block
	laptopNote   = "This result cannot be shown on the phone. Open it on the laptop."
)

var blockIDBad = regexp.MustCompile(`[^A-Za-z0-9_.:\-]+`)

// BlockOf turns one tools/call result of server's tool t into a block.
func BlockOf(server string, t Tool, res CallResult, o BlockOptions) blocks.Block {
	m := blockMaker{o: o}
	b := m.build(server, t, res)
	if b.Validate() == nil {
		return b
	}
	// Too big or a cap broken: fewer rows and items first.
	for n := len(b.Rows); n > 1 && b.Validate() != nil; n /= 2 {
		b.Rows, b.Truncated = b.Rows[:n/2], true
	}
	for n := len(b.Items); n > 1 && b.Validate() != nil; n /= 2 {
		b.Items, b.Truncated = b.Items[:n/2], true
	}
	if b.Validate() == nil {
		return b
	}
	return m.laptopOnly(b)
}

type blockMaker struct{ o BlockOptions }

// text is one string for a block: cleaned, redacted, one line, at most
// max runes.
func (m blockMaker) text(s string, max int) string {
	if m.o.Clean != nil {
		s = m.o.Clean(s)
	}
	s = ai.RedactSecrets(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
	s = strings.Join(strings.Fields(s), " ")
	return cutRunes(s, max)
}

// long is a text block's text: cleaned and redacted, lines kept.
func (m blockMaker) long(s string) string {
	if m.o.Clean != nil {
		s = m.o.Clean(s)
	}
	s = ai.RedactSecrets(strings.ToValidUTF8(s, "�"))
	if len(s) > maxBlockText {
		s = s[:maxBlockText]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		s += "\n…"
	}
	return s
}

// cutID makes s a block id: only [A-Za-z0-9_.:-], at most max bytes.
func cutID(s string, max int) string {
	s = blockIDBad.ReplaceAllString(s, "_")
	if len(s) > max {
		s = s[:max]
	}
	return s
}

func cutRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

func (m blockMaker) build(server string, t Tool, res CallResult) blocks.Block {
	id := m.o.ID
	if id == "" {
		id = "mcp"
	}
	title := t.Title
	if title == "" && t.Annotations != nil {
		title = t.Annotations.Title
	}
	if title == "" {
		title = t.Name
	}
	b := blocks.Block{
		ID:     cutID(id, blocks.MaxID),
		Title:  m.text(server+" · "+title, blocks.MaxTitle),
		Source: m.text(DisplayName(server, t.Name), blocks.MaxTitle),
		ForYou: m.text(res.UserText(), blocks.MaxForYou),
	}
	if strings.TrimSpace(b.Title) == "" {
		b.Title = "MCP result"
	}
	if res.IsError {
		return m.failed(b, res)
	}
	if len(res.StructuredContent) > 0 && json.Valid(res.StructuredContent) {
		return m.fromJSON(b, res.StructuredContent)
	}
	var texts []string
	var other []Content
	for _, i := range res.modelOrder() {
		c := res.Content[i]
		switch {
		case c.Type == "text":
			texts = append(texts, c.Text)
		case c.Type == "resource" && c.Resource != nil && c.Resource.Text != "":
			if strings.Contains(c.Resource.MimeType, "json") && json.Valid([]byte(c.Resource.Text)) && len(res.Content) == 1 {
				return m.fromJSON(b, json.RawMessage(c.Resource.Text))
			}
			texts = append(texts, c.Resource.Text)
		default:
			other = append(other, c)
		}
	}
	joined := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if len(texts) == 1 && len(other) == 0 && (strings.HasPrefix(joined, "{") || strings.HasPrefix(joined, "[")) && json.Valid([]byte(joined)) {
		return m.fromJSON(b, json.RawMessage(joined)) // text that is JSON
	}
	if joined == "" && len(other) > 0 {
		return m.contentList(b, other)
	}
	for _, c := range other { // links and media under the text
		joined += "\n\n" + m.contentLine(c)
	}
	joined = strings.TrimSpace(joined)
	if joined == "" {
		if b.ForYou != "" {
			b.Kind = blocks.KindText
			return b
		}
		b.Kind, b.State = blocks.KindStatus, blocks.ToneOK
		b.Items = []blocks.BlockItem{{ID: "done", Title: "Done, with no result to show", Tone: blocks.ToneOK}}
		return b
	}
	return m.textBlock(b, joined)
}

// failed is a red status with the error's text.
func (m blockMaker) failed(b blocks.Block, res CallResult) blocks.Block {
	b.Kind, b.State = blocks.KindStatus, blocks.ToneBad
	msg := strings.TrimSpace(res.render(nil))
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "error:"))
	first, rest, _ := strings.Cut(msg, "\n")
	if strings.TrimSpace(first) == "" {
		first = "The tool failed"
	}
	b.Items = []blocks.BlockItem{{ID: "error", Title: m.text(first, blocks.MaxTitle), Sub: m.text(rest, blocks.MaxSub), Tone: blocks.ToneBad}}
	return b
}

func (m blockMaker) textBlock(b blocks.Block, s string) blocks.Block {
	b.Kind = blocks.KindText
	s = m.long(s)
	if m.o.Markdown != nil {
		b.MD = m.o.Markdown(s)
	}
	if b.MD == nil {
		b.MD = plainDoc(s)
	}
	return b
}

// plainDoc is text as paragraphs, one per blank-line part.
func plainDoc(s string) *blocks.MdNode {
	doc := &blocks.MdNode{K: "doc"}
	for _, p := range strings.Split(s, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			doc.C = append(doc.C, blocks.MdNode{K: "p", C: []blocks.MdNode{{K: "text", Text: p}}})
		}
	}
	return doc
}

// contentLine is one link or media item as a line of text.
func (m blockMaker) contentLine(c Content) string {
	switch c.Type {
	case "resource_link":
		if c.Name != "" {
			return "Link " + c.Name + ": " + c.URI
		}
		return "Link: " + c.URI
	case "image", "audio":
		return fmt.Sprintf("[%s %s, %s — open on the laptop]", c.Type, c.MimeType, sizeText(base64.StdEncoding.DecodedLen(len(c.Data))))
	case "resource":
		if c.Resource != nil {
			return "[resource " + c.Resource.URI + " — open on the laptop]"
		}
	}
	return "[" + c.Type + " — open on the laptop]"
}

// contentList shows links and media as a list. A link carries a resource
// ref so the phone can send it back; media stay on the laptop.
func (m blockMaker) contentList(b blocks.Block, cs []Content) blocks.Block {
	b.Kind = blocks.KindList
	for i, c := range cs {
		it := blocks.BlockItem{ID: "c" + strconv.Itoa(i)}
		switch c.Type {
		case "resource_link":
			it.Title = m.text(c.Name, blocks.MaxTitle)
			if it.Title == "" {
				it.Title = m.text(c.URI, blocks.MaxTitle)
			}
			it.Sub = m.text(c.URI, blocks.MaxSub)
			if u := m.text(c.URI, blocks.MaxTitle); u != "" && u == strings.TrimSpace(c.URI) {
				it.Ref = &blocks.BlockRef{Kind: "resource", ID: u}
			}
		case "image", "audio":
			it.Title = m.text(c.Type+" "+c.MimeType, blocks.MaxTitle)
			it.Sub = sizeText(base64.StdEncoding.DecodedLen(len(c.Data))) + " · open on the laptop"
			it.Tone = blocks.ToneIdle
		default:
			it.Title = m.text(m.contentLine(c), blocks.MaxTitle)
		}
		if strings.TrimSpace(it.Title) == "" {
			it.Title = c.Type
		}
		if strings.TrimSpace(it.Title) == "" {
			it.Title = "item"
		}
		if len(b.Items) == blocks.MaxItems {
			b.Truncated = true
			break
		}
		b.Items = append(b.Items, it)
	}
	b.Total = len(cs)
	return b
}

// fromJSON picks the block for a JSON value.
func (m blockMaker) fromJSON(b blocks.Block, raw json.RawMessage) blocks.Block {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) > 0 && raw[0] == '[':
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil {
			return m.fromArray(b, raw, arr)
		}
	case len(raw) > 0 && raw[0] == '{':
		keys, err := objectKeys(raw)
		var obj map[string]json.RawMessage
		if err == nil && json.Unmarshal(raw, &obj) == nil {
			return m.fromObject(b, raw, keys, obj)
		}
	default:
		var v any
		if json.Unmarshal(raw, &v) == nil {
			return m.textBlock(b, scalarText(v))
		}
	}
	return m.jsonBlock(b, raw)
}

func (m blockMaker) fromArray(b blocks.Block, raw json.RawMessage, arr []json.RawMessage) blocks.Block {
	if len(arr) == 0 {
		b.Kind = blocks.KindList
		b.Items = []blocks.BlockItem{{ID: "none", Title: "No items", Tone: blocks.ToneIdle}}
		return b
	}
	objects, scalars := 0, 0
	for _, e := range arr {
		switch kindOf(e) {
		case '{':
			objects++
		case '[':
		default:
			scalars++
		}
	}
	switch {
	case objects == len(arr):
		return m.table(b, raw, arr)
	case scalars == len(arr):
		b.Kind = blocks.KindList
		for i, e := range arr {
			if i == blocks.MaxItems {
				b.Truncated = true
				break
			}
			var v any
			_ = json.Unmarshal(e, &v)
			t := m.text(scalarText(v), blocks.MaxTitle)
			if t == "" {
				t = "(empty)"
			}
			b.Items = append(b.Items, blocks.BlockItem{ID: "i" + strconv.Itoa(i), Title: t})
		}
		b.Total = len(arr)
		return b
	}
	return m.jsonBlock(b, raw)
}

// fromObject: an object with one list of objects is a table (with a
// total from "total" or "count"); a flat object is a detail.
func (m blockMaker) fromObject(b blocks.Block, raw json.RawMessage, keys []string, obj map[string]json.RawMessage) blocks.Block {
	if len(keys) == 0 {
		return m.textBlock(b, "(empty result)")
	}
	listKey := ""
	lists := 0
	for _, k := range keys {
		if kindOf(obj[k]) == '[' {
			lists++
			var arr []json.RawMessage
			if json.Unmarshal(obj[k], &arr) == nil && len(arr) > 0 && kindOf(arr[0]) == '{' {
				listKey = k
			}
		}
	}
	if lists == 1 && listKey != "" {
		var arr []json.RawMessage
		_ = json.Unmarshal(obj[listKey], &arr)
		out := m.fromArray(b, obj[listKey], arr)
		for _, k := range []string{"total", "total_count", "totalCount", "count"} {
			if n, err := strconv.Atoi(string(obj[k])); err == nil && n >= len(arr) {
				out.Total = n
				out.Truncated = out.Truncated || n > len(arr)
				break
			}
		}
		return out
	}
	b.Kind = blocks.KindDetail
	for _, k := range keys {
		if len(b.Fields) == blocks.MaxFields {
			b.Truncated = true
			break
		}
		label := m.text(k, blocks.MaxTitle)
		if label == "" {
			label = "(no name)"
		}
		v := obj[k]
		f := blocks.BlockField{Label: label, Value: m.text(cellText(v), blocks.MaxSub)}
		switch kindOf(v) {
		case '{', '[':
			f.Kind = blocks.ColMono
		case 'n':
			f.Kind = blocks.ColNum
		}
		if tone := toneOf(k, f.Value); tone != "" {
			f.Tone = tone
		}
		b.Fields = append(b.Fields, f)
	}
	b.Total = len(keys)
	return b
}

// table builds a table from objects: columns in the order first seen.
func (m blockMaker) table(b blocks.Block, raw json.RawMessage, arr []json.RawMessage) blocks.Block {
	b.Kind = blocks.KindTable
	var cols []string
	seen := map[string]bool{}
	rows := make([]map[string]json.RawMessage, 0, len(arr))
	for i, e := range arr {
		var obj map[string]json.RawMessage
		if json.Unmarshal(e, &obj) != nil {
			return m.jsonBlock(b, raw)
		}
		if i < blocks.MaxRows {
			rows = append(rows, obj)
		}
		keys, _ := objectKeys(e)
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	if len(cols) == 0 {
		return m.textBlock(b, "(empty rows)")
	}
	if len(cols) > blocks.MaxColumns {
		cols = cols[:blocks.MaxColumns]
	}
	ids := map[string]bool{}
	for _, k := range cols {
		id := cutID(k, blocks.MaxID-4)
		if id == "" || id == "_" {
			id = "col"
		}
		for base, n := id, 2; ids[id]; n++ {
			id = base + "_" + strconv.Itoa(n)
		}
		ids[id] = true
		label := m.text(k, blocks.MaxTitle)
		if label == "" {
			label = id
		}
		c := blocks.BlockColumn{ID: id, Label: label, Kind: blocks.ColText}
		num, has := true, false
		for _, r := range rows {
			if v, ok := r[k]; ok && string(v) != "null" {
				has = true
				num = num && kindOf(v) == 'n'
			}
		}
		switch low := strings.ToLower(k); {
		case has && num:
			c.Kind = blocks.ColNum
		case low == "id" || low == "sha" || low == "hash" || strings.HasSuffix(low, "_id"):
			c.Kind = blocks.ColMono
		}
		b.Columns = append(b.Columns, c)
	}
	for _, r := range rows {
		row := make([]string, len(cols))
		for j, k := range cols {
			if v, ok := r[k]; ok {
				row[j] = m.text(cellText(v), blocks.MaxSub)
			}
		}
		b.Rows = append(b.Rows, row)
	}
	b.Total = len(arr)
	b.Truncated = len(arr) > len(rows)
	return b
}

// jsonBlock shows the value as raw JSON, every string redacted; too big
// is a laptop note.
func (m blockMaker) jsonBlock(b blocks.Block, raw json.RawMessage) blocks.Block {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return m.laptopOnly(b)
	}
	out, err := json.MarshalIndent(m.redactValue(v, 0), "", "  ")
	if err != nil || len(out) > blocks.MaxJSON {
		return m.laptopOnly(b)
	}
	b.Kind, b.JSON = blocks.KindJSON, string(out)
	return b
}

// redactValue cleans and redacts every string (keys too) of a value.
func (m blockMaker) redactValue(v any, depth int) any {
	if depth > 32 {
		return "…"
	}
	switch x := v.(type) {
	case string:
		return m.long(x)
	case []any:
		for i := range x {
			x[i] = m.redactValue(x[i], depth+1)
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[m.long(k)] = m.redactValue(e, depth+1)
		}
		return out
	}
	return v
}

// laptopOnly keeps the title and says where to look.
func (m blockMaker) laptopOnly(b blocks.Block) blocks.Block {
	out := blocks.Block{ID: b.ID, Title: b.Title, Source: b.Source, Kind: blocks.KindText, ForYou: laptopNote}
	if out.Validate() != nil {
		out = blocks.Block{ID: "mcp", Title: "MCP result", Kind: blocks.KindText, ForYou: laptopNote}
	}
	return out
}

// kindOf is the JSON kind of a raw value: '{', '[', '"', 'n' (number),
// 'b' (bool), 0 (null or bad).
func kindOf(raw json.RawMessage) byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0
	}
	switch c := raw[0]; {
	case c == '{' || c == '[' || c == '"':
		return c
	case c == 't' || c == 'f':
		return 'b'
	case c == 'n':
		return 0
	}
	return 'n'
}

// cellText is a raw value as a cell: strings as they are, yes / no,
// numbers as sent, nested values as compact JSON.
func cellText(raw json.RawMessage) string {
	switch kindOf(raw) {
	case 0:
		return ""
	case '{', '[':
		var buf bytes.Buffer
		if json.Compact(&buf, raw) == nil {
			return buf.String()
		}
		return string(raw)
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return scalarText(v)
}

func scalarText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// toneOf colours a status-like field ("state": "failed" → bad).
func toneOf(key, value string) string {
	switch strings.ToLower(key) {
	case "status", "state", "conclusion", "result", "health":
	default:
		return ""
	}
	switch strings.ToLower(value) {
	case "ok", "success", "succeeded", "passed", "pass", "done", "healthy", "merged", "closed", "completed":
		return blocks.ToneOK
	case "failed", "failure", "fail", "error", "errored", "broken", "unhealthy", "cancelled", "canceled":
		return blocks.ToneBad
	case "running", "in_progress", "pending", "queued", "started", "busy":
		return blocks.ToneBusy
	case "warning", "warn", "degraded", "skipped":
		return blocks.ToneWarn
	case "open", "draft", "idle":
		return blocks.ToneInfo
	}
	return ""
}
