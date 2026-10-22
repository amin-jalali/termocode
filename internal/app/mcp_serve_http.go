package app

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// http_send for outside agents (M1 MS-7): the chat's tool with the chat's
// gate, unchanged (httpAIGate: GET / HEAD / OPTIONS to loopback run at
// once; other hosts and methods ask; private hosts always ask). Variables
// such as {{$secret NAME}} and private env values are resolved inside
// termocode, registered as secrets and hidden in the card, the audit line
// and the result. Two additions:
//
//   - the card says when the request carries such secret values, since
//     the card shows them hidden;
//   - a request that would send a saved secret is refused (in Validate,
//     before any card) unless its host (scheme, host, port) is one the
//     user's saved requests and environments already use: an outside
//     agent must not steer a secret to a host the user never set up;
//   - "Always allow" is per host (http_send:<host>, or <host>/write for
//     other methods), like the chat's class, never per tool.

// mcpServeHTTPSend is http_send with the card note.
func mcpServeHTTPSend(t ai.Tool, root string) ai.Tool {
	describe := t.Describe
	t.Describe = func(a map[string]any) string {
		text := ""
		if describe != nil {
			text = describe(a)
		}
		if n := len(httpAIGet(root, a).rr.Secrets); n > 0 {
			text += fmt.Sprintf("\nSends %d secret value%s from your env files or secret store (hidden here).", n, plurals(n))
		}
		return text
	}
	validate := t.Validate
	t.Validate = func(a map[string]any) error {
		if validate != nil {
			if err := validate(a); err != nil {
				return err
			}
		}
		r := httpAIGet(root, a)
		if len(r.rr.Secrets) == 0 {
			return nil
		}
		host := mcpServeHTTPOrigin(r.rr.URL)
		if host == "" || !mcpServeSavedHTTPOrigins(root)[host] {
			return &mcp.CallError{Code: mcp.CodeInvalidParams, Message: fmt.Sprintf(
				"this request sends a saved secret to %s, which is not used by any of your saved requests; "+
					"outside agents may only send secrets to hosts your saved requests already use", httpclient.RedactURL(hostOrRaw(host, r.rr.URL)))}
		}
		return nil
	}
	return t
}

func hostOrRaw(host, raw string) string {
	if host != "" {
		return host
	}
	return raw
}

// mcpServeHTTPOrigin is scheme://host[:port] of raw, lower case, with
// the default port dropped ("" when raw is not an http(s) URL).
func mcpServeHTTPOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if (scheme != "http" && scheme != "https") || host == "" {
		return ""
	}
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(host, ":") { // IPv6
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host
}

// mcpServeSavedHTTPOrigins collects the origins the workspace's saved
// requests (.http / .rest files, in every environment) and environment
// values already use. Secrets are not looked up here (a URL that needs a
// $secret does not count), and nothing is registered as secret.
func mcpServeSavedHTTPOrigins(root string) map[string]bool {
	out := map[string]bool{}
	add := func(raw string) {
		if o := mcpServeHTTPOrigin(raw); o != "" {
			out[o] = true
		}
	}
	for _, file := range httpWorkspaceFiles(root, 200) {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		f := httpclient.Parse(string(data))
		dir := filepath.Dir(file)
		envs := httpclient.LoadEnvs(dir, root)
		for _, env := range append([]string{""}, envs.Names()...) {
			rv := &httpclient.Resolver{FileVars: f.Vars, Env: env, Envs: envs}
			for _, req := range f.Requests {
				rr, _ := rv.Resolve(req)
				add(rr.URL)
			}
		}
	}
	envs := httpclient.LoadEnvs(root, root)
	for _, env := range append([]string{httpclient.SharedEnv}, envs.Names()...) {
		for _, name := range envs.VarNames(env) {
			if v, _, ok := envs.Lookup(env, name); ok {
				add(v)
			}
		}
	}
	return out
}

// mcpServeHTTPAlwaysKey is the "Always allow" key of a gated http_send
// call: per host from its class, "" when the class is "" (private hosts).
func mcpServeHTTPAlwaysKey(class string) string {
	host, ok := strings.CutPrefix(class, "http:")
	if !ok || host == "" {
		return ""
	}
	return "http_send:" + host
}

// mcpServeForgetHTTP drops the request http_send built for args
// (httpAICache is keyed by the map's address: a later map at the same
// address must never get this call's request).
func mcpServeForgetHTTP(args map[string]any) { httpAIForget(args) }
