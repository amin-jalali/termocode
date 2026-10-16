package services

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strings"
)

// The replacement every mask uses (the same word as ai.Redact).
const redacted = "[REDACTED]"

var (
	// scheme://user:PASS@host
	urlPassRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@"']*:)([^\s@/"']+)(@)`)
	// KEY=value or KEY: value where KEY looks secret.
	assignRe = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:pass|secret|token|key|dsn|auth|credential)[A-Z0-9_]*)(\s*[=:]\s*)("[^"\n]*"|'[^'\n]*'|[^\s"']+)`)
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

// EnvFileSecrets returns the values of secret-looking keys (≥ 8 chars)
// in a .env file, for ai.RegisterSecret.
func EnvFileSecrets(path string) []string {
	var out []string
	for k, v := range envLines(path) {
		if IsSecretKey(k) && len(v) >= 8 {
			out = append(out, v)
		}
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
