package migrate

import (
	"sort"
	"strings"
)

// termocode expands snippets with Neovim's vim.snippet, which reads the LSP
// snippet syntax: tab stops ($1, ${1}), placeholders (${1:text}, nested),
// choices (${1|a,b|}) and these variables. VS Code uses the same syntax but
// has more variables and regex transforms, which Neovim cannot fill.
var snippetVariables = map[string]bool{
	"TM_SELECTED_TEXT": true, "TM_CURRENT_LINE": true, "TM_CURRENT_WORD": true,
	"TM_LINE_INDEX": true, "TM_LINE_NUMBER": true, "TM_FILENAME": true,
	"TM_FILENAME_BASE": true, "TM_DIRECTORY": true, "TM_FILEPATH": true,
}

type snipNode struct {
	kind    byte // 't' text, 'n' tab stop, 'p' placeholder, 'c' choice, 'v' variable
	text    string
	index   string // the digits of a tab stop
	name    string // a variable name
	kids    []snipNode
	choices []string
}

type snipParser struct {
	s       string
	pos     int
	unknown map[string]bool // variables termocode cannot fill
	regex   bool            // a transform was found
	mixed   bool            // a placeholder holds text and another construct
}

// ConvertSnippetBody turns a VS Code snippet body into a body termocode's
// snippet engine accepts. Text is re-escaped so that nothing can fail when
// the snippet expands: a "$" or "{" that does not start a construct stays
// literal text. A variable termocode cannot fill is replaced by its default
// when it has one. A body that needs an unknown variable or a regex
// transform is not converted: the result is "" and reason says why.
func ConvertSnippetBody(body string) (converted, reason string) {
	p := &snipParser{s: body, unknown: map[string]bool{}}
	nodes := p.normalize(p.nodes(false))
	if p.regex || p.mixed || len(p.unknown) > 0 {
		var parts []string
		if p.mixed {
			parts = append(parts, "has a placeholder that mixes text and another placeholder, which termocode cannot expand")
		}
		if p.regex {
			parts = append(parts, "uses a regex transform")
		}
		if len(p.unknown) > 0 {
			var names []string
			for n := range p.unknown {
				names = append(names, "$"+n)
			}
			sort.Strings(names)
			parts = append(parts, "uses "+strings.Join(names, ", ")+", which termocode cannot fill")
		}
		return "", strings.Join(parts, " and ") + "; not imported"
	}
	var b strings.Builder
	emitSnippet(&b, nodes, false)
	return b.String(), ""
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isNameStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
func isNameChar(c byte) bool { return isNameStart(c) || isDigit(c) }

// normalize joins neighbouring text, and puts the text of "${0:text}" after
// "$0". Neovim's parser takes a placeholder or variable default that is one
// piece (all text, or one construct); anything else is marked mixed.
func (p *snipParser) normalize(in []snipNode) []snipNode {
	var out []snipNode
	add := func(n snipNode) {
		if n.kind == 't' && len(out) > 0 && out[len(out)-1].kind == 't' {
			out[len(out)-1].text += n.text
			return
		}
		if n.kind != 't' || n.text != "" {
			out = append(out, n)
		}
	}
	for _, n := range in {
		n.kids = p.normalize(n.kids)
		if n.kind == 'p' && n.index == "0" {
			add(snipNode{kind: 'n', index: "0"})
			for _, k := range n.kids {
				add(k)
			}
			continue
		}
		if (n.kind == 'p' || n.kind == 'v') && len(n.kids) > 1 {
			p.mixed = true
		}
		add(n)
	}
	return out
}

// nodes parses until the end, or until the "}" that closes a placeholder.
func (p *snipParser) nodes(inBraces bool) []snipNode {
	var out []snipNode
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, snipNode{kind: 't', text: text.String()})
			text.Reset()
		}
	}
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case c == '\\' && p.pos+1 < len(p.s) && strings.IndexByte("$}\\", p.s[p.pos+1]) >= 0:
			text.WriteByte(p.s[p.pos+1])
			p.pos += 2
		case c == '}' && inBraces:
			flush()
			return out
		case c == '$':
			start := p.pos
			if n, ok := p.construct(); ok {
				flush()
				out = append(out, n...)
			} else {
				p.pos = start + 1
				text.WriteByte('$') // not a construct: a literal dollar
			}
		default:
			text.WriteByte(c)
			p.pos++
		}
	}
	flush()
	return out
}

// construct parses what starts at "$". ok=false: it is plain text.
func (p *snipParser) construct() ([]snipNode, bool) {
	s := p.s
	i := p.pos + 1
	if i >= len(s) {
		return nil, false
	}
	switch {
	case isDigit(s[i]):
		j := i
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		p.pos = j
		return []snipNode{{kind: 'n', index: s[i:j]}}, true
	case isNameStart(s[i]):
		j := i
		for j < len(s) && isNameChar(s[j]) {
			j++
		}
		p.pos = j
		return p.variable(s[i:j], nil), true
	case s[i] != '{':
		return nil, false
	}
	i++
	j := i
	if j < len(s) && isDigit(s[j]) {
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		return p.braced(s[i:j], j, false)
	}
	if j < len(s) && isNameStart(s[j]) {
		for j < len(s) && isNameChar(s[j]) {
			j++
		}
		return p.braced(s[i:j], j, true)
	}
	return nil, false
}

// braced parses what follows the name or number inside "${".
func (p *snipParser) braced(name string, j int, isVar bool) ([]snipNode, bool) {
	s := p.s
	if j >= len(s) {
		return nil, false
	}
	switch s[j] {
	case '}':
		p.pos = j + 1
		if isVar {
			return p.variable(name, nil), true
		}
		return []snipNode{{kind: 'n', index: name}}, true
	case ':':
		save := *p
		p.pos = j + 1
		kids := p.nodes(true)
		if p.pos >= len(s) { // never closed: plain text
			*p = save
			return nil, false
		}
		p.pos++ // the closing brace
		if isVar {
			return p.variable(name, kids), true
		}
		return []snipNode{{kind: 'p', index: name, kids: kids}}, true
	case '/':
		// ${1/regex/format/options} or ${VAR/regex/format/options}
		p.regex = true
		p.pos = len(s)
		return nil, true
	case '|':
		if isVar {
			return nil, false
		}
		var choices []string
		var cur strings.Builder
		for k := j + 1; k < len(s); k++ {
			switch {
			case s[k] == '\\' && k+1 < len(s) && strings.IndexByte(",|\\", s[k+1]) >= 0:
				cur.WriteByte(s[k+1])
				k++
			case s[k] == ',':
				choices = append(choices, cur.String())
				cur.Reset()
			case s[k] == '|' && k+1 < len(s) && s[k+1] == '}':
				choices = append(choices, cur.String())
				if len(choices) == 1 && choices[0] == "" {
					return nil, false
				}
				p.pos = k + 2
				return []snipNode{{kind: 'c', index: name, choices: choices}}, true
			default:
				cur.WriteByte(s[k])
			}
		}
	}
	return nil, false
}

// variable is a known variable, or what stands in for an unknown one.
func (p *snipParser) variable(name string, def []snipNode) []snipNode {
	if snippetVariables[name] {
		return []snipNode{{kind: 'v', name: name, kids: def}}
	}
	if def != nil {
		return def // the default replaces a variable termocode cannot fill
	}
	p.unknown[name] = true
	return nil
}

// emitSnippetText escapes text. A "}" needs it only inside a placeholder.
func emitSnippetText(b *strings.Builder, s string, inBraces bool) {
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '$' || s[i] == '\\' || s[i] == '}' && inBraces:
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
}

func emitSnippet(b *strings.Builder, nodes []snipNode, inBraces bool) {
	for i, n := range nodes {
		// "$1" cannot be followed by a character that would read as part of it.
		bare := true
		if i+1 < len(nodes) && nodes[i+1].kind == 't' && isNameChar(nodes[i+1].text[0]) {
			bare = false
		}
		switch n.kind {
		case 't':
			emitSnippetText(b, n.text, inBraces)
		case 'n':
			if bare {
				b.WriteString("$" + n.index)
			} else {
				b.WriteString("${" + n.index + "}")
			}
		case 'p':
			b.WriteString("${" + n.index + ":")
			emitSnippet(b, n.kids, true)
			b.WriteString("}")
		case 'c':
			b.WriteString("${" + n.index + "|")
			for k, c := range n.choices {
				if k > 0 {
					b.WriteByte(',')
				}
				b.WriteString(strings.NewReplacer("\\", "\\\\", ",", "\\,", "|", "\\|").Replace(c))
			}
			b.WriteString("|}")
		case 'v':
			b.WriteString("${" + n.name)
			if len(n.kids) > 0 {
				b.WriteByte(':')
				emitSnippet(b, n.kids, true)
			}
			b.WriteString("}")
		}
	}
}
