package codeindex

import (
	"strings"
	"unicode"
)

// Tokens splits text into lower-case words: identifiers are cut at
// camelCase, snake_case and punctuation ("parseHTTPRequest" → parse,
// http, request), words shorter than two letters and stop words are
// dropped, and plural / -ing / -ed forms are folded (stem).
func Tokens(s string) []string {
	var out []string
	emit := func(w []rune) {
		if len(w) < 2 {
			return
		}
		l := strings.ToLower(string(w))
		if stopWords[l] {
			return
		}
		t := stem(l)
		if len(t) < 2 || stopWords[t] {
			return
		}
		out = append(out, t)
	}
	rs := []rune(s)
	var cur []rune
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			emit(cur)
			cur = cur[:0]
			continue
		}
		if len(cur) > 0 && unicode.IsUpper(r) {
			prev := cur[len(cur)-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			// fooBar → foo|Bar; HTTPServer → HTTP|Server
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && nextLower {
				emit(cur)
				cur = cur[:0]
			}
		}
		cur = append(cur, r)
	}
	emit(cur)
	return out
}

// stem folds the most common English endings: plural s / ies, -ing,
// -ed and a final e ("deleted", "deletes", "delete" → "delet"). It is
// deliberately light: a wrong fold only joins two near words.
func stem(w string) string {
	n := len(w)
	if n > 4 && strings.HasSuffix(w, "ies") {
		return w[:n-3] + "y"
	}
	if n > 3 && w[n-1] == 's' && !strings.ContainsRune("su'i", rune(w[n-2])) {
		w = w[:n-1]
	}
	for _, suf := range []string{"ing", "ed"} {
		if len(w) > len(suf)+2 && strings.HasSuffix(w, suf) && !strings.HasSuffix(w, "eed") {
			w = w[:len(w)-len(suf)]
			if m := len(w); m > 3 && w[m-1] == w[m-2] && !strings.ContainsRune("aeiouls", rune(w[m-1])) {
				w = w[:m-1] // running → run, stopped → stop
			}
			break
		}
	}
	if m := len(w); m > 3 && w[m-1] == 'e' {
		w = w[:m-1]
	}
	return w
}

// stopWords are English filler and the keywords of common languages:
// they say nothing about what the code does.
var stopWords = set(
	"the", "an", "and", "or", "of", "to", "in", "on", "at", "by", "for", "with", "from", "into", "as", "is", "are", "be",
	"it", "its", "this", "that", "these", "those", "there", "here", "what", "which", "who", "how", "why", "when", "where",
	"do", "does", "not", "no", "yes", "all", "any", "some", "one", "can", "will", "should", "would", "if", "else", "then",
	"we", "you", "my", "our", "me", "so", "but", "than", "too", "very", "just", "only", "also", "out", "up", "about",
	"func", "function", "return", "var", "let", "const", "type", "struct", "interface", "package", "import", "export",
	"class", "def", "fn", "pub", "self", "new", "nil", "null", "none", "true", "false", "int", "int64", "int32", "uint",
	"string", "bool", "byte", "float64", "float32", "void", "public", "private", "static", "final", "go", "defer",
	"range", "for", "while", "switch", "case", "default", "break", "continue", "elif", "end", "begin", "use", "mod",
	"https", "www", "com", "todo",
)

// related groups words that mean nearly the same in code. A query word
// also looks for the other words of its groups, at half weight.
var related = [][]string{
	{"auth", "authenticate", "login", "logout", "signin", "sign", "credential", "password", "passwd", "token", "oauth", "session"},
	{"delete", "remove", "rm", "drop", "erase", "purge", "destroy", "unlink", "clear"},
	{"error", "err", "fail", "failure", "panic", "exception", "fault", "crash"},
	{"config", "configuration", "setting", "cfg", "conf", "option", "pref", "preference"},
	{"db", "database", "sql", "query", "table", "row", "record", "column", "schema"},
	{"request", "api", "endpoint", "url", "fetch", "client", "server", "route", "handler"},
	{"test", "spec", "assert", "check", "verify", "expect"},
	{"start", "launch", "spawn", "run", "exec", "execute", "begin", "boot", "init"},
	{"stop", "kill", "cancel", "terminate", "abort", "quit", "exit", "shutdown"},
	{"save", "write", "store", "persist", "flush", "dump"},
	{"load", "read", "open", "parse", "decode", "import"},
	{"user", "account", "owner", "person", "member", "profile"},
	{"path", "dir", "directory", "folder", "filename"},
	{"git", "commit", "branch", "repo", "repository", "merge", "rebase"},
	{"container", "docker", "image", "pod", "compose"},
	{"secret", "key", "mask", "redact", "vault", "encrypt"},
	{"ui", "view", "render", "draw", "paint", "screen", "panel", "widget", "layout", "display"},
	{"time", "timeout", "deadline", "clock", "duration", "timer", "schedule", "cron"},
	{"cache", "memo", "memoize"},
	{"log", "logger", "trace", "debug", "audit"},
	{"send", "post", "publish", "emit", "notify", "message"},
	{"find", "search", "lookup", "match", "grep", "filter"},
	{"list", "enumerate", "items", "collection"},
	{"create", "make", "add", "insert", "build"},
	{"update", "change", "modify", "edit", "patch", "set"},
	{"copy", "clone", "duplicate"},
	{"retry", "backoff", "again"},
	{"lock", "mutex", "sync", "race", "concurrent"},
	{"network", "socket", "tcp", "connection", "conn", "dial"},
	{"key", "keymap", "keybinding", "shortcut", "hotkey"},
}

// relatedOf is each (stemmed) word's related words.
var relatedOf = func() map[string][]string {
	m := map[string][]string{}
	for _, g := range related {
		var ws []string
		for _, w := range g {
			ws = append(ws, stem(w))
		}
		for _, a := range ws {
			for _, b := range ws {
				if a != b && !contains(m[a], b) {
					m[a] = append(m[a], b)
				}
			}
		}
	}
	return m
}()

// queryVector is the query's word weights: its own words at 1 (each
// time it is typed), their related words at 0.5.
func queryVector(q string) map[string]float64 {
	v := map[string]float64{}
	words := Tokens(q)
	for _, t := range words {
		v[t]++
	}
	for _, t := range words {
		for _, r := range relatedOf[t] {
			if v[r] < 0.5 {
				v[r] = 0.5
			}
		}
	}
	return v
}

func set(ws ...string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
