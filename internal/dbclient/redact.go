package dbclient

import (
	"net/url"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

var (
	urlCredential      = lazyre.New(`(?i)([a-z][a-z0-9+.-]*://[^@\s/?#]*:)([^@\s]*)(@[^/\s?#]+)`)
	dsnCredential      = lazyre.New(`(?i)([^\s:/@]+:)([^\n]*)(@(?:tcp6?|unix)\()`)
	userPathCredential = lazyre.New(`(?i)([^\s:/@]+:)([^@\s]+)(@/)`)
	assignmentSecret   = lazyre.New(`(?i)([a-z0-9_]*(?:password|passwd|passphrase|pwd|pass|secret|token|api[_-]?key))([\t ]*[=:][\t ]*)('[^\n]*'|"(?:\\.|[^"\\])*"|[^\s,;&}]*)`)
	jsonSecret         = lazyre.New(`(?i)("[a-z0-9_]*(?:password|passwd|passphrase|pwd|secret|token|api[_-]?key)"\s*:\s*")(?:\\.|[^"\\])*(")`)
	sqlSecret          = lazyre.New(`(?i)\b(password|identified\s+by)(\s+)('(?:''|[^'])*'|"(?:""|[^"])*"|[^\s;,]+)`)
	redisCommand       = lazyre.New(`(?i)\b(CONFIG\s+SET\s+requirepass|AUTH2\s+\S+|requirepass|masterauth)\s+\S+`)
	bearerSecret       = lazyre.New(`(?i)(authorization\s*:\s*bearer\s+)\S+`)
	shortPassword      = lazyre.New(`(?i)(^|\s)-p([^\s]+)`)
)

// Redact hides passwords, tokens and keys in s (connection URLs and
// DSNs, key=value pairs, JSON fields, SQL, Redis AUTH lines). A pattern
// runs only when s holds a word every match of it needs: result grids
// redact every cell, and most cells hold none.
func Redact(s string) string {
	k := newFoldKeys(s)
	at := strings.IndexByte(s, '@') >= 0
	// Handle whole URLs through net/url so escaped credentials remain valid.
	if at {
		if u, err := url.Parse(s); err == nil && u.User != nil {
			if _, ok := u.User.Password(); ok {
				u.User = url.UserPassword(u.User.Username(), "[redacted]")
				s = u.String()
			}
		}
		if k.has("://") {
			s = urlCredential.ReplaceAllString(s, "$1[redacted]$3")
		}
		s = dsnCredential.ReplaceAllString(s, "$1[redacted]$3")
		if strings.Contains(s, "@/") {
			s = userPathCredential.ReplaceAllString(s, "$1[redacted]$3")
		}
	}
	secretWord := k.has("pass", "pwd", "secret", "token", "api")
	if secretWord {
		s = jsonSecret.ReplaceAllString(s, "$1[redacted]$2")
	}
	if k.has("requirepass", "auth2", "masterauth") {
		s = redisCommand.ReplaceAllString(s, "$1 [redacted]")
	}
	if k.has("bearer") {
		s = bearerSecret.ReplaceAllString(s, "$1[redacted]")
	}
	if k.has("-p") {
		s = shortPassword.ReplaceAllString(s, "${1}-p[redacted]")
	}
	if secretWord {
		s = assignmentSecret.ReplaceAllStringFunc(s, func(match string) string {
			loc := assignmentSecret.FindStringSubmatchIndex(match)
			if loc == nil {
				return match
			}
			name := strings.ToLower(match[loc[2]:loc[3]])
			value := strings.Trim(strings.ToLower(match[loc[6]:loc[7]]), `\"')`)
			if strings.Contains(name, "password") && (value == "yes" || value == "no") && strings.Contains(strings.ToLower(s), "using password:") {
				return match
			}
			return match[:loc[6]] + "[redacted]"
		})
	}
	if k.has("password", "identified") {
		s = sqlSecret.ReplaceAllStringFunc(s, func(match string) string {
			loc := sqlSecret.FindStringSubmatchIndex(match)
			if loc == nil {
				return match
			}
			name, sep, value := match[loc[2]:loc[3]], match[loc[4]:loc[5]], match[loc[6]:loc[7]]
			if strings.EqualFold(name, "password") && strings.EqualFold(value, "authentication") {
				return match
			}
			if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') {
				return name + sep + string(value[0]) + "[redacted]" + string(value[0])
			}
			return name + sep + "[redacted]"
		})
	}
	if !k.has("auth") {
		return s
	}
	return redactAuthLines(s)
}

// foldKeys answers "may s contain this word, ignoring case" the way the
// (?i) patterns match. Only two non-ASCII letters fold to ASCII ones
// (ſ to s, the Kelvin sign K to k); a string with either says yes to
// every word, so the full patterns decide.
type foldKeys struct {
	lower string // s with ASCII letters lower-cased
	all   bool
}

func newFoldKeys(s string) foldKeys {
	if strings.Contains(s, "ſ") || strings.Contains(s, "\u212a") {
		return foldKeys{all: true}
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if c := b[j]; c >= 'A' && c <= 'Z' {
					b[j] = c + 'a' - 'A'
				}
			}
			return foldKeys{lower: string(b)}
		}
	}
	return foldKeys{lower: s}
}

// has reports whether one of words (lower-case ASCII) may be in s.
func (k foldKeys) has(words ...string) bool {
	if k.all {
		return true
	}
	for _, w := range words {
		if strings.Contains(k.lower, w) {
			return true
		}
	}
	return false
}

func redactAuthLines(s string) string {
	var out strings.Builder
	for len(s) > 0 {
		n := strings.IndexByte(s, '\n')
		line, rest := s, ""
		if n >= 0 {
			line, rest = s[:n], s[n:]
		}
		if strings.HasSuffix(line, "\r") {
			line = redactAuthLine(strings.TrimSuffix(line, "\r")) + "\r"
		} else {
			line = redactAuthLine(line)
		}
		out.WriteString(line)
		if rest != "" {
			out.WriteByte('\n')
			s = rest[1:]
		} else {
			s = ""
		}
	}
	return out.String()
}

func redactAuthLine(line string) string {
	tokens := make([][2]int, 0, 3)
	for i := 0; i < len(line); {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		start := i
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		if start < i {
			tokens = append(tokens, [2]int{start, i})
		}
	}
	for i := len(tokens) - 2; i >= 0; i-- {
		if !strings.EqualFold(line[tokens[i][0]:tokens[i][1]], "AUTH") {
			continue
		}
		for j := len(tokens) - 1; j > i; j-- {
			t := tokens[j]
			line = line[:t[0]] + "[redacted]" + line[t[1]:]
		}
		return line
	}
	return line
}
