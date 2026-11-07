package httpclient

// Response chaining: {{name.response.status}},
// {{name.response.headers.X-Name}}, {{name.response.body.*}},
// {{name.response.body.$.json.path}} and
// {{name.response.body.//xml/path}} read the last response of the
// request called name (`# @name name`). The values come from
// Resolver.Responses; with no response (or no match) the placeholder
// stays raw and is listed as unresolved, like any unknown name.

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"slices"
	"strconv"
	"strings"
)

// chainRef is one parsed {{name.response.<part>}}.
type chainRef struct {
	name string
	part string // "status", "headers" or "body"
	arg  string // header name, or the body path ("*", "$...", "/...")
}

const respMark = ".response."

// parseChainRef reads expr (the text inside {{ }}).
func parseChainRef(expr string) (chainRef, bool) {
	i := strings.Index(expr, respMark)
	if i <= 0 {
		return chainRef{}, false
	}
	name, rest := expr[:i], expr[i+len(respMark):]
	if !isPlainName(name) {
		return chainRef{}, false
	}
	if rest == "status" {
		return chainRef{name: name, part: "status"}, true
	}
	part, arg, ok := strings.Cut(rest, ".")
	if !ok || arg == "" || (part != "headers" && part != "body") {
		return chainRef{}, false
	}
	return chainRef{name: name, part: part, arg: arg}, true
}

// chainValue evaluates expr against rv.Responses. secret reports a
// value that must be hidden (a secret header, or a body key that looks
// like a token / password).
func (rv *Resolver) chainValue(expr string) (val string, secret, ok bool) {
	if rv.Responses == nil {
		return "", false, false
	}
	ref, ok := parseChainRef(expr)
	if !ok {
		return "", false, false
	}
	resp, ok := rv.Responses(ref.name)
	if !ok || resp == nil {
		return "", false, false
	}
	switch ref.part {
	case "status":
		if resp.Status == 0 {
			return "", false, false
		}
		return strconv.Itoa(resp.Status), false, true
	case "headers":
		n := strings.ToLower(ref.arg)
		for _, h := range resp.Headers {
			if strings.ToLower(h.Name) == n {
				return h.Value, SecretHeader(n), true
			}
		}
		return "", false, false
	}
	switch {
	case ref.arg == "*":
		return string(resp.Body), false, true
	case strings.HasPrefix(ref.arg, "$"):
		v, ok := JSONPath(resp.Body, ref.arg)
		return v, ok && SecretHeader(lastPathKey(ref.arg)), ok
	case strings.HasPrefix(ref.arg, "/"):
		v, ok := XPath(resp.Body, ref.arg)
		return v, ok && SecretHeader(lastPathKey(ref.arg)), ok
	}
	return "", false, false
}

// lastPathKey is the last name in a JSON or XML path ("token" for
// $.data.token, "key" for $['key'], "x" for //a/@x).
func lastPathKey(p string) string {
	p = strings.TrimRight(p, "]")
	if i := strings.LastIndexAny(p, ".[/@"); i >= 0 {
		p = p[i+1:]
	}
	return strings.Trim(p, `'"`)
}

// ResponseRefs lists the request names that req reads a response from
// (sorted, each once). The caller can offer to send those first.
func ResponseRefs(req Request) []string {
	var out []string
	scan := func(s string) {
		if !strings.Contains(s, respMark) {
			return
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
			if ref, ok := parseChainRef(m[1]); ok && !slices.Contains(out, ref.name) {
				out = append(out, ref.name)
			}
		}
	}
	scan(req.URL)
	for _, h := range req.Headers {
		scan(h.Value)
	}
	if req.Body != nil {
		scan(*req.Body)
	}
	slices.Sort(out)
	return out
}

// ── JSONPath (subset) ───────────────────────────────────────────────────

// JSONPath reads one value from a JSON body. Supported: $, .name,
// ['name'] / ["name"], [n] and [-n] (from the end). A string comes back
// without quotes; anything else as compact JSON text (numbers keep their
// written form). Wildcards, filters and .. are not supported.
func JSONPath(body []byte, path string) (string, bool) {
	if !strings.HasPrefix(path, "$") {
		return "", false
	}
	cur := json.RawMessage(bytes.TrimSpace(body))
	if !json.Valid(cur) {
		return "", false
	}
	rest := path[1:]
	for rest != "" {
		var next json.RawMessage
		var ok bool
		switch rest[0] {
		case '.':
			rest = rest[1:]
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				end = len(rest)
			}
			key := rest[:end]
			if key == "" || key == "*" {
				return "", false
			}
			next, ok = jsonKey(cur, key)
			rest = rest[end:]
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return "", false
			}
			in := rest[1:end]
			if len(in) >= 2 && (in[0] == '\'' || in[0] == '"') && in[len(in)-1] == in[0] {
				next, ok = jsonKey(cur, in[1:len(in)-1])
			} else {
				n, err := strconv.Atoi(in)
				if err != nil {
					return "", false
				}
				next, ok = jsonIndex(cur, n)
			}
			rest = rest[end+1:]
		default:
			return "", false
		}
		if !ok {
			return "", false
		}
		cur = next
	}
	if len(cur) > 0 && cur[0] == '"' {
		var s string
		if json.Unmarshal(cur, &s) != nil {
			return "", false
		}
		return s, true
	}
	var b bytes.Buffer
	if json.Compact(&b, cur) != nil {
		return "", false
	}
	return b.String(), true
}

func jsonKey(cur json.RawMessage, key string) (json.RawMessage, bool) {
	var m map[string]json.RawMessage
	if len(cur) == 0 || cur[0] != '{' || json.Unmarshal(cur, &m) != nil {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

func jsonIndex(cur json.RawMessage, n int) (json.RawMessage, bool) {
	var a []json.RawMessage
	if len(cur) == 0 || cur[0] != '[' || json.Unmarshal(cur, &a) != nil {
		return nil, false
	}
	if n < 0 {
		n += len(a)
	}
	if n < 0 || n >= len(a) {
		return nil, false
	}
	return a[n], true
}

// ── XPath (subset) ──────────────────────────────────────────────────────

type xmlNode struct {
	name  string
	attrs []xml.Attr
	kids  []*xmlNode
	parts []xmlPart // own text and children, in order
}

// xmlPart is a run of text, or (kid >= 0) the kid-th child element.
type xmlPart struct {
	text string
	kid  int
}

// textAll is all character data inside n, trimmed.
func (n *xmlNode) textAll() string {
	var b strings.Builder
	n.writeText(&b)
	return strings.TrimSpace(b.String())
}

func (n *xmlNode) writeText(b *strings.Builder) {
	for _, p := range n.parts {
		if p.kid >= 0 {
			n.kids[p.kid].writeText(b)
		} else {
			b.WriteString(p.text)
		}
	}
}

// maxXMLDepth stops a hostile body from nesting without end.
const maxXMLDepth = 256

func parseXML(body []byte) (*xmlNode, bool) {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = false
	doc := &xmlNode{}
	stack := []*xmlNode{doc}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) > maxXMLDepth {
				return nil, false
			}
			n := &xmlNode{name: t.Name.Local, attrs: t.Attr}
			top.parts = append(top.parts, xmlPart{kid: len(top.kids)})
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 1 {
				top.parts = append(top.parts, xmlPart{text: string(t), kid: -1})
			}
		}
	}
	if len(doc.kids) == 0 {
		return nil, false
	}
	return doc, true
}

// XPath reads one value from an XML body: the text of the first
// matching element, or an attribute. Supported: /a/b, //b (anywhere),
// name[n] (1-based), *, and a last step of @attr or text().
func XPath(body []byte, path string) (string, bool) {
	if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return "", false
	}
	doc, ok := parseXML(body)
	if !ok {
		return "", false
	}
	nodes := []*xmlNode{doc}
	steps := strings.Split(path[1:], "/")
	deep := false
	for i, st := range steps {
		if st == "" {
			if deep {
				return "", false
			}
			deep = true
			continue
		}
		last := i == len(steps)-1
		if last && !deep && (st == "text()" || strings.HasPrefix(st, "@")) {
			if len(nodes) == 0 || nodes[0] == doc {
				return "", false
			}
			if st == "text()" {
				return nodes[0].textAll(), true
			}
			for _, a := range nodes[0].attrs {
				if a.Name.Local == st[1:] {
					return a.Value, true
				}
			}
			return "", false
		}
		name, pos, ok := xpathStep(st)
		if !ok {
			return "", false
		}
		ctx := nodes
		if deep {
			ctx = nil
			for _, n := range nodes {
				ctx = appendSelfAndBelow(ctx, n)
			}
			deep = false
		}
		nodes = nil
		for _, n := range ctx {
			k := 0
			for _, c := range n.kids {
				if name != "*" && c.name != name {
					continue
				}
				k++
				if pos == 0 || k == pos {
					nodes = append(nodes, c)
				}
			}
		}
		if len(nodes) == 0 {
			return "", false
		}
	}
	if deep || len(nodes) == 0 || nodes[0] == doc {
		return "", false
	}
	return nodes[0].textAll(), true
}

// xpathStep reads "name" or "name[n]" (pos 0 = every match).
func xpathStep(st string) (name string, pos int, ok bool) {
	name = st
	if i := strings.IndexByte(st, '['); i >= 0 {
		if !strings.HasSuffix(st, "]") {
			return "", 0, false
		}
		n, err := strconv.Atoi(st[i+1 : len(st)-1])
		if err != nil || n < 1 {
			return "", 0, false
		}
		name, pos = st[:i], n
	}
	if name == "" || strings.ContainsAny(name, "[]()@") {
		return "", 0, false
	}
	return name, pos, true
}

func appendSelfAndBelow(out []*xmlNode, n *xmlNode) []*xmlNode {
	out = append(out, n)
	for _, c := range n.kids {
		out = appendSelfAndBelow(out, c)
	}
	return out
}
