package app

import "strings"

// The find bar's ".*" mode takes the regex syntax of VS Code (JavaScript):
// "fo+b", "(a|b)", "\d{2}", "$1" in the replacement. nvim's default
// ("magic") syntax reads +, ?, |, ( and { as literal characters, so the
// pattern is translated to nvim's "very magic" (\v) syntax, where they
// are operators as in JavaScript.

// jsRegexToVim translates a JavaScript-style regex into an nvim \v pattern
// usable in both a search (@/) and a :s/…/…/ command.
func jsRegexToVim(p string) string {
	var b strings.Builder
	b.WriteString(`\v`)
	rs := []rune(p)
	inClass := false
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == '\\' && i+1 < len(rs) {
			i++
			c := rs[i]
			if inClass {
				// nvim reads \d inside [] as a decimal char code.
				switch c {
				case 'd':
					b.WriteString("0-9")
				case 'w':
					b.WriteString("0-9A-Za-z_")
				case 's':
					b.WriteString(` \t`)
				default:
					b.WriteRune('\\')
					b.WriteRune(c)
				}
				continue
			}
			switch c {
			case 'b':
				b.WriteString("%(<|>)")
			case '/':
				b.WriteString(`\/`)
			default:
				b.WriteRune('\\')
				b.WriteRune(c)
			}
			continue
		}
		if inClass {
			if r == ']' {
				inClass = false
			}
			b.WriteRune(r)
			continue
		}
		switch r {
		case '[':
			inClass = true
			b.WriteRune(r)
			// A ] right after [ or [^ is a literal member.
			if i+1 < len(rs) && rs[i+1] == '^' {
				i++
				b.WriteRune('^')
			}
			if i+1 < len(rs) && rs[i+1] == ']' {
				i++
				b.WriteString(`\]`)
			}
		case '(':
			if strings.HasPrefix(string(rs[i:]), "(?:") {
				b.WriteString("%(")
				i += 2
			} else {
				b.WriteRune(r)
			}
		case '*', '+', '?':
			if i+1 < len(rs) && rs[i+1] == '?' { // lazy quantifier
				i++
				b.WriteString(map[rune]string{'*': "{-}", '+': "{-1,}", '?': "{-0,1}"}[r])
			} else {
				b.WriteRune(r)
			}
		case '<', '>', '=', '@', '%', '~', '&', '/':
			// Operators in \v, plain characters in JavaScript.
			b.WriteRune('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// jsReplaceToVim translates a VS Code regex replacement ($1, $&, \n) into
// the right side of an nvim :s command.
func jsReplaceToVim(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '$' && i+1 < len(rs) && rs[i+1] >= '0' && rs[i+1] <= '9':
			i++
			b.WriteRune('\\')
			b.WriteRune(rs[i])
		case r == '$' && i+1 < len(rs) && rs[i+1] == '&':
			i++
			b.WriteString(`\0`)
		case r == '$' && i+1 < len(rs) && rs[i+1] == '$':
			i++
			b.WriteRune('$')
		case r == '\\' && i+1 < len(rs) && rs[i+1] == 'n':
			i++
			b.WriteString(`\r`)
		case r == '\\' && i+1 < len(rs) && rs[i+1] == 't':
			i++
			b.WriteRune('\t')
		default:
			b.WriteString(vimReplaceEscape(string(r)))
		}
	}
	return b.String()
}
