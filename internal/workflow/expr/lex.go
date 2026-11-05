package expr

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type tokKind int

const (
	tEOF tokKind = iota
	tNum
	tStr
	tName
	tOp
)

type token struct {
	kind tokKind
	text string  // name, operator, or the decoded string
	num  float64 // for tNum
	pos  int     // byte offset, for errors
}

func errAt(pos int, format string, args ...any) error {
	return fmt.Errorf("expr: col %d: %s", pos+1, fmt.Sprintf(format, args...))
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// lex splits src into tokens. It never reads past src and does one pass.
func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isDigit(c):
			start := i
			for i < len(src) && isDigit(src[i]) {
				i++
			}
			if i+1 < len(src) && src[i] == '.' && isDigit(src[i+1]) {
				i++
				for i < len(src) && isDigit(src[i]) {
					i++
				}
			}
			if i < len(src) && isNameStart(src[i]) {
				return nil, errAt(i, "bad number")
			}
			f, err := parseNumber(src[start:i])
			if err != nil {
				return nil, errAt(start, "bad number")
			}
			toks = append(toks, token{kind: tNum, num: f, text: src[start:i], pos: start})
		case isNameStart(c):
			start := i
			for i < len(src) && isNameChar(src[i]) {
				i++
			}
			toks = append(toks, token{kind: tName, text: src[start:i], pos: start})
		case c == '"' || c == '\'':
			s, n, err := lexString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tStr, text: s, pos: i})
			i = n
		default:
			start := i
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=":
				toks = append(toks, token{kind: tOp, text: two, pos: start})
				i += 2
				continue
			}
			switch c {
			case '<', '>', '+', '-', '(', ')', '[', ']', ',', '.':
				toks = append(toks, token{kind: tOp, text: string(c), pos: start})
				i++
			default:
				r, _ := utf8.DecodeRuneInString(src[i:])
				return nil, errAt(i, "unexpected %q", r)
			}
		}
	}
	toks = append(toks, token{kind: tEOF, pos: len(src)})
	return toks, nil
}

// lexString reads a quoted string at src[i]. Escapes: \" \' \\ \n \t \r.
func lexString(src string, i int) (string, int, error) {
	q := src[i]
	start := i
	i++
	var b strings.Builder
	for i < len(src) {
		c := src[i]
		switch {
		case c == q:
			return b.String(), i + 1, nil
		case c == '\\':
			if i+1 >= len(src) {
				return "", 0, errAt(start, "string is not closed")
			}
			switch src[i+1] {
			case '"', '\'', '\\':
				b.WriteByte(src[i+1])
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				return "", 0, errAt(i, "unknown escape \\%c (use \\\" \\' \\\\ \\n \\t \\r)", src[i+1])
			}
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", 0, errAt(start, "string is not closed")
}
