package services

import (
	"bufio"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// The replacement every mask uses (the same word as ai.Redact).
const redacted = "[REDACTED]"

var (
	// scheme://user:PASS@host
	urlPassRe = lazyre.New(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@"']*:)([^\s@/"']+)(@)`)
	// KEY=value or KEY: value where KEY looks secret.
	assignRe = lazyre.New(`(?i)\b([A-Z0-9_]*(?:pass|secret|token|key|dsn|auth|credential)[A-Z0-9_]*)(\s*[=:]\s*)("[^"\n]*"|'[^'\n]*'|[^\s"']+)`)
)

// Redact masks URL passwords and secret-looking KEY=value assignments.
// It returns the text and how many values it masked. The app runs
// ai.Redact first (registered secrets, API keys), then this.
func Redact(s string) (string, int) {
	n := 0
	s = urlPassRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := urlPassRe.FindStringSubmatch(m)
		if sub[2] == redacted {
			return m
		}
		n++
		return sub[1] + redacted + sub[3]
	})
	s = assignRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := assignRe.FindStringSubmatch(m)
		v := strings.Trim(sub[3], `"'`)
		if v == "" || strings.Contains(v, redacted) {
			return m
		}
		n++
		return sub[1] + sub[2] + redacted
	})
	return s, n
}

// EnvFileValues reads the KEY=value pairs of a .env file (nil when it
// cannot be read). The secret store's ${dotenv:NAME} uses it.
func EnvFileValues(path string) map[string]string { return envLines(path) }

// envLines reads KEY=value pairs from a .env file.
func envLines(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out
}

// SecretValue is what of one env value must be hidden from the model:
// the password of a URL (the rest of a URL is the diagnosis), else the
// whole value when the key looks secret. Values under 8 characters are
// never secrets (they would shred normal text).
func SecretValue(key, value string) []string {
	value = strings.TrimSpace(value)
	if u, err := url.Parse(value); err == nil && u.Scheme != "" && u.Host != "" {
		if p, ok := u.User.Password(); ok && len(p) >= 8 {
			return []string{p}
		}
		return nil
	}
	if IsSecretKey(key) && !urlKey.MatchString(key) && len(value) >= 8 {
		return []string{value}
	}
	return nil
}

// urlKey names keys that hold an address: only a password inside is secret.
var urlKey = lazyre.New(`(?i)(^|_)(url|uri|dsn|host|addr|address|endpoint)$`)

// EnvFileSecrets returns the secret parts of a .env file's values (see
// SecretValue), for ai.RegisterSecret.
func EnvFileSecrets(path string) []string {
	var out []string
	for k, v := range envLines(path) {
		out = append(out, SecretValue(k, v)...)
	}
	sort.Strings(out)
	return out
}

// EnvFileKeys returns the key names of a .env file, sorted.
func EnvFileKeys(path string) []string {
	var out []string
	for k := range envLines(path) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
