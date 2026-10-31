// Package pdftext pulls the plain text out of a PDF file, in pure Go and
// without dependencies (OMX-32). It reads every content stream (raw or
// FlateDecode) and keeps the strings of the text operators Tj, TJ, ' and
// ". It is a best effort: fonts that map glyphs through a ToUnicode
// table (many CID fonts) or scanned pages give little or no text.
package pdftext

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"strings"
)

// MaxStream caps how much one stream may inflate to.
const MaxStream = 8 << 20

// ErrNotPDF is returned for data without the %PDF header.
var ErrNotPDF = errors.New("not a PDF file")

// Extract returns the text of a PDF ("" when it has none).
func Extract(data []byte) (string, error) {
	if !bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("%PDF")) {
		return "", ErrNotPDF
	}
	var out strings.Builder
	rest := data
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			break
		}
		// "endstream" also has "stream" in it: skip it.
		if i >= 3 && string(rest[i-3:i]) == "end" {
			rest = rest[i+6:]
			continue
		}
		dict := rest[max(0, i-512):i]
		if d := bytes.LastIndex(dict, []byte("<<")); d >= 0 {
			dict = dict[d:]
		}
		body := rest[i+6:]
		body = bytes.TrimPrefix(body, []byte("\r"))
		body = bytes.TrimPrefix(body, []byte("\n"))
		end := bytes.Index(body, []byte("endstream"))
		if end < 0 {
			break
		}
		raw := body[:end]
		rest = body[end+9:]
		if bytes.Contains(dict, []byte("/Image")) || bytes.Contains(dict, []byte("/DCTDecode")) {
			continue
		}
		if bytes.Contains(dict, []byte("/FlateDecode")) {
			z, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				continue
			}
			raw, err = io.ReadAll(io.LimitReader(z, MaxStream))
			if err != nil && len(raw) == 0 {
				continue
			}
		}
		if t := contentText(raw); t != "" {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(t)
		}
	}
	return strings.TrimSpace(out.String()), nil
}

// contentText reads the text operators of one content stream.
func contentText(c []byte) string {
	if !bytes.Contains(c, []byte("BT")) {
		return ""
	}
	var out strings.Builder
	var pending []string // strings since the last operator
	line := func() {
		if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
			out.WriteString("\n")
		}
	}
	for i := 0; i < len(c); {
		ch := c[i]
		switch {
		case ch == '(':
			s, n := literal(c[i:])
			pending = append(pending, s)
			i += n
		case ch == '<' && i+1 < len(c) && c[i+1] != '<':
			s, n := hexString(c[i:])
			pending = append(pending, s)
			i += n
		case ch == '%':
			for i < len(c) && c[i] != '\n' && c[i] != '\r' {
				i++
			}
		case isSpace(ch) || ch == '[' || ch == ']':
			i++
		default:
			j := i
			for j < len(c) && !isSpace(c[j]) && !strings.ContainsRune("()<>[]/%", rune(c[j])) {
				j++
			}
			if j == i {
				j++ // a lone delimiter such as '/' or '>'
			}
			op := string(c[i:j])
			i = j
			switch op {
			case "Tj", "TJ":
				out.WriteString(strings.Join(pending, ""))
			case "'", "\"":
				line()
				out.WriteString(strings.Join(pending, ""))
			case "Td", "TD", "T*", "Tm":
				line()
			case "ET":
				line()
			default:
				if !isNumber(op) && !strings.HasPrefix(op, "/") {
					pending = pending[:0]
				}
				continue
			}
			pending = pending[:0]
		}
	}
	return strings.TrimSpace(out.String())
}

// literal reads a (string) with escapes and nested parentheses; n is
// the bytes used.
func literal(c []byte) (string, int) {
	var b strings.Builder
	depth := 0
	i := 0
	for ; i < len(c); i++ {
		ch := c[i]
		switch ch {
		case '(':
			depth++
			if depth == 1 {
				continue
			}
		case ')':
			depth--
			if depth == 0 {
				return b.String(), i + 1
			}
		case '\\':
			i++
			if i >= len(c) {
				break
			}
			switch e := c[i]; e {
			case 'n':
				b.WriteByte('\n')
			case 'r', 'b', 'f':
			case 't':
				b.WriteByte('\t')
			case '\r', '\n':
			default:
				if e >= '0' && e <= '7' {
					v, k := 0, 0
					for k < 3 && i < len(c) && c[i] >= '0' && c[i] <= '7' {
						v = v*8 + int(c[i]-'0')
						i++
						k++
					}
					i--
					writeByte(&b, byte(v))
				} else {
					b.WriteByte(e)
				}
			}
			continue
		}
		writeByte(&b, ch)
	}
	return b.String(), i
}

// hexString reads a <hex> string; n is the bytes used.
func hexString(c []byte) (string, int) {
	end := bytes.IndexByte(c, '>')
	if end < 0 {
		return "", len(c)
	}
	var digits []byte
	for _, ch := range c[1:end] {
		if v, ok := hexVal(ch); ok {
			digits = append(digits, v)
		}
	}
	if len(digits)%2 == 1 {
		digits = append(digits, 0)
	}
	var b strings.Builder
	for k := 0; k+1 < len(digits); k += 2 {
		writeByte(&b, digits[k]<<4|digits[k+1])
	}
	return b.String(), end + 1
}

// writeByte keeps printable Latin-1 text and line breaks.
func writeByte(b *strings.Builder, v byte) {
	switch {
	case v == '\n' || v == '\t':
		b.WriteByte(v)
	case v >= 0x20 && v < 0x7f:
		b.WriteByte(v)
	case v >= 0xa0:
		b.WriteRune(rune(v))
	}
}

func hexVal(ch byte) (byte, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0', true
	case ch >= 'a' && ch <= 'f':
		return ch - 'a' + 10, true
	case ch >= 'A' && ch <= 'F':
		return ch - 'A' + 10, true
	}
	return 0, false
}

func isSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '\f' || ch == 0
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' {
			return false
		}
	}
	return true
}
