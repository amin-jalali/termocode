package httpclient

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParseHTTPFile reads any text as a .http file. Serialized requests
// read back to the same text, and every request has a method and a URL.
func FuzzParseHTTPFile(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "*.http"))
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(string(b))
		}
	}
	for _, s := range []string{
		"GET https://x/a\nAccept: */*\n\n{\"a\":1}\n###\n### named\nPOST {{host}}/b HTTP/1.1\n",
		"@host = https://api\n// @name list\n# @timeout 10s\n{{host}}/users\n",
		"###\n###\n",
		"POST https://x\n\n###not a separator\nbody\n",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file := Parse(src)
		for _, r := range file.Requests {
			if r.Method == "" || r.URL == "" {
				t.Fatalf("request without method or URL: %+v", r)
			}
			_ = r.Label()
		}
		s1 := Serialize(file.Requests)
		s2 := Serialize(Parse(s1).Requests)
		if s1 != s2 {
			t.Fatalf("serialize round trip changed:\n%q\n%q", s1, s2)
		}
	})
}

// FuzzQuickAndCurl reads any one-line request and any curl command.
// Neither may panic, and a curl line from ToCurl reads back the same
// method, URL and body.
func FuzzQuickAndCurl(f *testing.F) {
	for _, s := range []string{
		"GET https://x/a?b=1",
		"post https://x {\"a\":1}",
		`curl -X POST 'https://x/a' -H 'Content-Type: application/json' --data '{"a":"b c"}'`,
		`curl https://x -d a=1 -d b=2 -G`,
		`curl "unterminated`,
		`curl -u user:pass --json '{}' https://x`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		_, _ = ParseQuick(line)
		r, err := FromCurl(line)
		if err != nil || r.URL == "" || r.Method == "" {
			return
		}
		back, err := FromCurl(ToCurl(Resolved{Request: r}, true))
		if err != nil {
			t.Fatalf("ToCurl output does not read back: %v\n%s", err, ToCurl(Resolved{Request: r}, true))
		}
		if back.Method != r.Method || back.URL != r.URL || back.BodyText() != r.BodyText() {
			t.Fatalf("curl round trip:\n%+v\n%+v", r, back)
		}
	})
}

// FuzzIndentPrefix pretty-prints any body: the output keeps to max, and
// a complete valid JSON document matches json.Indent.
func FuzzIndentPrefix(f *testing.F) {
	for _, s := range []string{`{"a":[1,2,{"b":null}],"c":"x\"y"}`, `[]`, `{}`, ` [ {} , [ ] ] `, `{"a":`, `nope`} {
		f.Add([]byte(s), uint16(40))
	}
	f.Fuzz(func(t *testing.T, src []byte, m uint16) {
		max := int(m)
		out, complete, ok := IndentPrefix(src, max)
		if !ok {
			return
		}
		if len(out) > max {
			t.Fatalf("output %d bytes over max %d", len(out), max)
		}
		if !complete || !json.Valid(src) {
			return
		}
		var want bytes.Buffer
		if err := json.Indent(&want, bytes.TrimSpace(src), "", "  "); err != nil {
			t.Fatal(err)
		}
		if want.Len() <= max && !bytes.Equal(out, want.Bytes()) {
			t.Fatalf("indent differs:\n%s\n---\n%s", out, want.Bytes())
		}
	})
}

// FuzzRedactURL hides passwords and secret query values in any text,
// and redacting twice changes nothing more.
func FuzzRedactURL(f *testing.F) {
	for _, s := range []string{
		"https://me:hunter2@x.com/a?token=abc&x=1",
		"see postgres://u:p@h/db and https://x?api_key=k#f",
		"https://x?sig=1;key=2",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r := RedactURL(s)
		if RedactURL(r) != r {
			t.Fatalf("not stable: %q -> %q -> %q", s, r, RedactURL(r))
		}
		if u, err := url.Parse(s); err == nil && u.User != nil && u.Scheme != "" && !strings.ContainsAny(s, " \t\n") {
			if p, has := u.User.Password(); has && p != "" && len(p) > 3 && strings.Contains(r, ":"+p+"@") {
				t.Fatalf("password %q left in %q", p, r)
			}
		}
		_ = HostKey(s)
		_ = ClassifyHost(s)
	})
}

// FuzzEnvFiles reads any http-client.env.json and .env text without a
// panic.
func FuzzEnvFiles(f *testing.F) {
	f.Add([]byte(`{"dev":{"host":"https://x","token":"t"},"$shared":{"a":"b"}}`), []byte("A=1\nexport B=\"two words\"\n# c\nC='x'\n"))
	f.Add([]byte(`{"dev":[]}`), []byte("=\n=x\nA\n"))
	f.Fuzz(func(t *testing.T, envJSON, dotenv []byte) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "http-client.env.json"), envJSON, 0o600)
		_ = os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), envJSON, 0o600)
		_ = os.WriteFile(filepath.Join(dir, ".env"), dotenv, 0o600)
		e := LoadEnvs(dir, dir)
		for _, n := range e.Names() {
			for _, v := range e.VarNames(n) {
				_, _, _ = e.Lookup(n, v)
			}
		}
		_ = os.WriteFile(filepath.Join(dir, "state.json"), envJSON, 0o600)
		_, _ = LoadState(filepath.Join(dir, "state.json"))
	})
}
