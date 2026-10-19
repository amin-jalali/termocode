package dbclient

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	urlCredential      = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^@\s/?#]*:)([^@\s]*)(@[^/\s?#]+)`)
	dsnCredential      = regexp.MustCompile(`(?i)([^\s:/@]+:)([^\n]*)(@(?:tcp6?|unix)\()`)
	userPathCredential = regexp.MustCompile(`(?i)([^\s:/@]+:)([^@\s]+)(@/)`)
	assignmentSecret   = regexp.MustCompile(`(?i)([a-z0-9_]*(?:password|passwd|passphrase|pwd|pass|secret|token|api[_-]?key))([\t ]*[=:][\t ]*)('[^\n]*'|"(?:\\.|[^"\\])*"|[^\s,;&}]*)`)
	jsonSecret         = regexp.MustCompile(`(?i)("[a-z0-9_]*(?:password|passwd|passphrase|pwd|secret|token|api[_-]?key)"\s*:\s*")(?:\\.|[^"\\])*(")`)
	sqlSecret          = regexp.MustCompile(`(?i)\b(password|identified\s+by)(\s+)('(?:''|[^'])*'|"(?:""|[^"])*"|[^\s;,]+)`)
	redisCommand       = regexp.MustCompile(`(?i)\b(CONFIG\s+SET\s+requirepass|AUTH2\s+\S+|requirepass|masterauth)\s+\S+`)
	bearerSecret       = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)\S+`)
	shortPassword      = regexp.MustCompile(`(?i)(^|\s)-p([^\s]+)`)
)

func Redact(s string) string {
	// Handle whole URLs through net/url so escaped credentials remain valid.
	if u, err := url.Parse(s); err == nil && u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "[redacted]")
			s = u.String()
		}
	}
	s = urlCredential.ReplaceAllString(s, "$1[redacted]$3")
	s = dsnCredential.ReplaceAllString(s, "$1[redacted]$3")
	s = userPathCredential.ReplaceAllString(s, "$1[redacted]$3")
	s = jsonSecret.ReplaceAllString(s, "$1[redacted]$2")
	s = redisCommand.ReplaceAllString(s, "$1 [redacted]")
	s = bearerSecret.ReplaceAllString(s, "$1[redacted]")
	s = shortPassword.ReplaceAllString(s, "${1}-p[redacted]")
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
	return redactAuthLines(s)
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
