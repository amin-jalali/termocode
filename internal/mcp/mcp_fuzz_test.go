package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/secrets"
)

// FuzzParseFile reads any bytes as mcp.json. Only non-JSON is an error,
// a project file never gets a runnable ${cmd:} server, and every usable
// server has a valid name.
func FuzzParseFile(f *testing.F) {
	for _, s := range []string{
		`{"mcpServers":{"gh":{"type":"http","url":"https://api.githubcopilot.com/mcp/","headers":{"Authorization":"Bearer ${secret:gh}"}}}}`,
		`{"mcpServers":{"pg":{"command":"npx","args":["-y","pg"],"env":{"K":"sk-abcdefghijklmnop"}}}}`,
		`{"mcpServers":{"x":{"command":"${cmd:echo hi}"}}}`,
		`{"mcpServers":{"bad name!":{"command":"a"}}}`,
		`{"mcpServers":{"a":{"url":"http://x","command":"y"}}}`,
		`{"mcpServers":{"a":{"type":"sse","url":"https://x","auth":{"gh":"me"}}}}`,
		`{"mcpServers":{"a":7}}`,
		`{"mcpServers":[]}`,
		`[]`,
		`not json`,
	} {
		f.Add([]byte(s), false)
		f.Add([]byte(s), true)
	}
	f.Fuzz(func(t *testing.T, b []byte, user bool) {
		source := SourceProject
		if user {
			source = SourceUser
		}
		list, _, err := ParseFile(b, source, "mcp.json")
		if err != nil {
			return
		}
		for _, s := range list {
			if s.Problem != "" {
				continue
			}
			if !nameRe.MatchString(s.Name) {
				t.Fatalf("server %q has no problem but a bad name", s.Name)
			}
			if !user && secrets.HasCmdRef(string(s.raw)) {
				t.Fatalf("project server %q may run ${cmd:}", s.Name)
			}
			if !user && s.Auth != nil && s.Auth.GH != "" {
				t.Fatalf("project server %q may use a gh login", s.Name)
			}
		}
	})
}

// importShape decodes a pasted block the way ParseImport does, so the
// fuzz test can compare the entries before and after secrets move out.
func importShape(text string) map[string]map[string]any {
	text = strings.TrimSuffix(strings.TrimSpace(text), ",")
	var top map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &top) != nil && json.Unmarshal([]byte("{"+text+"}"), &top) != nil {
		return nil
	}
	if raw, ok := top["mcpServers"]; ok {
		top = nil
		_ = json.Unmarshal(raw, &top)
	}
	out := map[string]map[string]any{}
	for n, raw := range top {
		var e map[string]any
		if json.Unmarshal(raw, &e) == nil {
			out[n] = e
		}
	}
	return out
}

// FuzzParseImport pastes any text. Imported entries keep no literal
// token, and every moved value comes back exactly from Secrets (two
// fields never share one secret name).
func FuzzParseImport(f *testing.F) {
	for _, s := range []string{
		`{"mcpServers":{
		"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/","headers":{"Authorization":"Bearer ghp_abcdefghijklmnopqrstuvwxyz123456"}},
		"pg":{"command":"npx","args":["-y","pg-mcp"],"env":{"PG_URL":"postgres://me@localhost/db","API_KEY":"sk-proj-abcdefghijklmnopqrstuv"}},
		"bad":{"type":"http","url":"http://evil.example/mcp"}}}`,
		`"one": {"command": "x", "env": {"T": "ghp_abc"}},`,
		`{"mcpServers":3}`,
		`{}`,
		`nope`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		list, _, err := ParseImport(text)
		if err != nil {
			return
		}
		orig := importShape(text)
		for _, im := range list {
			b, _ := json.Marshal(im.Entry)
			if w := secretLiteral(parseEntry(im.Name, b)); w != "" {
				t.Fatalf("%s keeps a literal secret in %s: %s", im.Name, w, b)
			}
			before := orig[im.Name]
			if before == nil {
				t.Fatalf("imported %q is not in the text", im.Name)
			}
			check := func(where string, was, now any) {
				ws, ok1 := was.(string)
				ns, ok2 := now.(string)
				if !ok1 || !ok2 || ws == ns {
					return
				}
				for name, val := range im.Secrets {
					ref := "${secret:" + name + "}"
					if ns == ref && val == ws {
						return
					}
					if scheme, ok := strings.CutSuffix(ns, " "+ref); ok && scheme+" "+val == ws {
						return
					}
				}
				t.Fatalf("%s %s: %q became %q; secrets %q do not give it back", im.Name, where, ws, ns, im.Secrets)
			}
			for _, field := range []string{"headers", "env"} {
				was, _ := before[field].(map[string]any)
				now, _ := im.Entry[field].(map[string]any)
				for k, v := range now {
					check(field+"."+k, was[k], v)
				}
			}
			was, _ := before["oauth"].(map[string]any)
			now, _ := im.Entry["oauth"].(map[string]any)
			if was != nil && now != nil {
				check("oauth.client_secret", was["client_secret"], now["client_secret"])
			}
		}
	})
}

// FuzzParseDuration: an accepted duration is never negative and reads
// back the same from its own text.
func FuzzParseDuration(f *testing.F) {
	for _, s := range []string{"0", "90s", "10m", " 1h30m ", "-1s", "", "1.5h", "9999999999h"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, ok := ParseDuration(s)
		if !ok {
			return
		}
		if d < 0 {
			t.Fatalf("%q gave negative %v", s, d)
		}
		if d2, ok := ParseDuration(d.String()); !ok || d2 != d {
			t.Fatalf("%v reads back as %v, %v", d, d2, ok)
		}
	})
}

// FuzzAuthHeaders reads any WWW-Authenticate value and any gh status
// text without a panic.
func FuzzAuthHeaders(f *testing.F) {
	f.Add(`Bearer resource_metadata="https://x/.well-known/oauth-protected-resource", scope="a b", error="invalid_token"`, "github.com\n  ✓ Logged in to github.com account me (keyring)\n  - Active account: true\n")
	f.Add(`Bearer realm="a\"b\\", x=y,,=z`, "")
	f.Add(`resource_metadata=`, "  ✓ Logged in to github.com account \n")
	f.Fuzz(func(t *testing.T, h, gh string) {
		if parseAuthenticate(h) == nil {
			t.Fatal("parseAuthenticate returned nil")
		}
		_ = ParseGHStatus(gh)
	})
}

// FuzzSSEScan reads any server-sent event stream for the answer to id 1.
func FuzzSSEScan(f *testing.F) {
	for _, s := range []string{
		"id: 4\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n",
		": keep-alive\r\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\r\n\r\ndata: {\"id\":1,\r\ndata: \"error\":{\"code\":-1,\"message\":\"x\"}}\n\n",
		"data: not json\n\nid: 9\n",
		"data:{\"id\":1,\"result\":null}",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		c := &httpConn{}
		res, found, _, err := c.scan(bytes.NewReader(b), json.RawMessage("1"))
		if !found && res != nil {
			t.Fatalf("result %q without an answer", res)
		}
		_ = err
	})
}
