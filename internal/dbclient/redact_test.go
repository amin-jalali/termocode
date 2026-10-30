package dbclient

import (
	"math/rand"
	"net/url"
	"strings"
	"testing"
)

// redactReference is Redact as it was before its keyword checks: every
// pattern runs on every string. Redact must give the same result.
func redactReference(s string) string {
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

// redactCorpus holds strings with and without secrets: every pattern's
// trigger, mixed case, the Unicode letters that fold to ASCII (ſ, K),
// multi-line text and plain cells.
var redactCorpus = []string{
	"", "42", "名前", "line one\nline two", "v1→7", "plain text, no secret",
	"postgres://bob:s3cr3t@db.example.com:5432/app", "POSTGRES://Bob:x@host/db",
	"mysql user:pw@tcp(127.0.0.1:3306)/db", "user:pw@unix(/tmp/sock)/db", "user:pw@/dbname",
	"password=hunter2", "PASSWORD: 'quoted secret'", "db_pass = abc;", "pwd=1", "api-key: k", "API_KEY=zzz",
	`{"password": "x\"y", "token":"t"}`, `{"Secret" : "s"}`, "the token is here", "a secret", "passport", "apiary",
	"ALTER USER bob IDENTIFIED BY 'p''w'", "create user x password \"q\"", "password authentication failed",
	"CONFIG SET requirepass hunter2", "AUTH2 user pass", "masterauth abc", "requirepass xyz",
	"Authorization: Bearer abc.def", "authorization:bearer   tok", "psql -h h -pSECRET", "-Pfoo", "x -p",
	"AUTH hunter2", "auth user pw", "redis> AUTH default pw\nOK", "line\r\nAUTH x\r\n",
	"paſsword=x", "toKen=y", "ſecret: z", "Access denied (using password: YES)", "password: no (using password: no)",
	"https://user@host/path", "ftp://a:b@c", "http://x/@y", "me@example.com", "a@b c:d@/e",
}

func TestRedactSameAsReference(t *testing.T) {
	for _, s := range redactCorpus {
		if got, want := Redact(s), redactReference(s); got != want {
			t.Errorf("Redact(%q) = %q, want %q", s, got, want)
		}
	}
	// Random mixes of the pieces the patterns look for.
	pieces := []string{"pass", "PASS", "word", "wd", "pwd", "secret", "token", "api", "_", "-", "key", "=", ":", " ", "\t",
		"'", "\"", "@", "/", "://", "tcp(", "unix(", "identified", " by ", "auth", "AUTH", "2", "requirepass", "masterauth",
		"bearer", "authorization", "-p", "x", "名", "ſ", "K", "\n", "\r\n", "{", "}", ";", ",", "&", "http", "s3", "#", "?"}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for n := r.Intn(12); n >= 0; n-- {
			b.WriteString(pieces[r.Intn(len(pieces))])
		}
		s := b.String()
		if got, want := Redact(s), redactReference(s); got != want {
			t.Fatalf("Redact(%q) = %q, want %q", s, got, want)
		}
	}
}

// BenchmarkRedactCell is Redact on typical result cells (no secret).
func BenchmarkRedactCell(b *testing.B) {
	cells := []string{"42", "Alice Johnson", "line one\nline two 名前", "v17→119", "2026-10-30 12:00:00", "true"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Redact(cells[i%len(cells)])
	}
}
