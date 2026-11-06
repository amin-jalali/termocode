package git

import (
	"net/url"
	"strings"
)

// Autolinks (GITX-29, O24, P11): #123, issue keys (ABC-123) and commit
// hashes in a line of text. The finders are strict on purpose: a word,
// a version or a plain number must not become a link.

// LinkKind is what a Ref names.
type LinkKind int

const (
	LinkIssue LinkKind = iota + 1 // #123: an issue or PR of the remote
	LinkKey                       // ABC-123: an issue of an outside tracker
	LinkSHA                       // 1a2b3c4: a commit hash (7 to 40 hex)
)

// AutoLink is one link-like word of a line; Start..End is its byte span
// (with the # of an issue), Text is the number, key or hash.
type AutoLink struct {
	Kind       LinkKind
	Text       string
	Start, End int
}

// notKeys are common names in KEY-N form that are not issue keys.
var notKeys = map[string]bool{
	"UTF": true, "SHA": true, "ISO": true, "CVE": true, "RFC": true, "MD": true,
	"X": true, "IPV": true, "GPT": true, "PEP": true, "ECMA": true, "CP": true,
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isHex(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' }

// AutoLinkAt is the Ref under byte offset off of line.
func AutoLinkAt(line string, off int) (AutoLink, bool) {
	if off < 0 || off >= len(line) {
		return AutoLink{}, false
	}
	// The run of word bytes, '-' and '#' around off.
	in := func(b byte) bool { return isWordByte(b) || b == '-' || b == '#' }
	if !in(line[off]) {
		return AutoLink{}, false
	}
	s, e := off, off
	for s > 0 && in(line[s-1]) {
		s--
	}
	for e < len(line) && in(line[e]) {
		e++
	}
	for _, r := range ParseAutoLinks(line[s:e]) {
		r.Start += s
		r.End += s
		if off >= r.Start && off < r.End {
			if r.Kind == LinkIssue && r.Start > 0 && (line[r.Start-1] == '&' || line[r.Start-1] == '/') {
				return AutoLink{}, false // &#123; or a URL fragment
			}
			return r, true
		}
	}
	return AutoLink{}, false
}

// ParseAutoLinks lists the Refs of line, left to right.
func ParseAutoLinks(line string) []AutoLink {
	var out []AutoLink
	n := len(line)
	for i := 0; i < n; {
		if i > 0 && (isWordByte(line[i-1]) || line[i-1] == '-') {
			i++
			continue
		}
		if r, ok := autoLinkFrom(line, i); ok {
			if r.Kind != LinkIssue || i == 0 || (line[i-1] != '&' && line[i-1] != '/') {
				out = append(out, r)
			}
			i = r.End
			continue
		}
		i++
	}
	return out
}

// autoLinkFrom is the Ref that starts at i (a word start), if any.
func autoLinkFrom(line string, i int) (AutoLink, bool) {
	n := len(line)
	end := func(j int) bool { return j == n || !isWordByte(line[j]) && line[j] != '-' }
	if line[i] == '#' {
		j := i + 1
		for j < n && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j > i+1 && j-i-1 <= 7 && end(j) {
			return AutoLink{Kind: LinkIssue, Text: line[i+1 : j], Start: i, End: j}, true
		}
		return AutoLink{}, false
	}
	// KEY-123: a capital, then capitals or digits, a dash, digits.
	if line[i] >= 'A' && line[i] <= 'Z' {
		j := i + 1
		for j < n && (line[j] >= 'A' && line[j] <= 'Z' || line[j] >= '0' && line[j] <= '9') {
			j++
		}
		if j < n && line[j] == '-' && j-i <= 10 {
			k := j + 1
			for k < n && line[k] >= '0' && line[k] <= '9' {
				k++
			}
			if k > j+1 && k-j-1 <= 7 && end(k) && !notKeys[line[i:j]] {
				return AutoLink{Kind: LinkKey, Text: line[i:k], Start: i, End: k}, true
			}
		}
		return AutoLink{}, false
	}
	// A hash: 7 to 40 lower-case hex with a digit and a letter.
	j := i
	digit, letter := false, false
	for j < n && isHex(line[j]) {
		if line[j] <= '9' {
			digit = true
		} else {
			letter = true
		}
		j++
	}
	if j-i >= 7 && j-i <= 40 && digit && letter && end(j) {
		return AutoLink{Kind: LinkSHA, Text: line[i:j], Start: i, End: j}, true
	}
	return AutoLink{}, false
}

// IssueLink is the web page of issue (or PR) n of the remote page base.
// GitHub sends /issues/N on to the PR when N is one.
func IssueLink(base, n string) string {
	if isGitLab(base) {
		return base + "/-/issues/" + n
	}
	return base + "/issues/" + n
}

// KeyLink is the web page of issue key in tmpl, an http(s) link with
// {key} in it (or the key added at the end). ok=false: no usable tmpl.
func KeyLink(tmpl, key string) (string, bool) {
	tmpl = strings.TrimSpace(tmpl)
	u, err := url.Parse(strings.ReplaceAll(tmpl, "{key}", "KEY"))
	if tmpl == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", false
	}
	if strings.Contains(tmpl, "{key}") {
		return strings.ReplaceAll(tmpl, "{key}", url.PathEscape(key)), true
	}
	return tmpl + url.PathEscape(key), true
}
