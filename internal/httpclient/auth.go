package httpclient

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Auth schemes for `# @auth` and the `$auth` env variable.
const (
	AuthNone   = "none"
	AuthBasic  = "basic"
	AuthBearer = "bearer"
	AuthAPIKey = "apikey"
	AuthDigest = "digest"
	AuthOAuth1 = "oauth1"
)

// AuthEnvVar is the env variable (http-client.env.json, per env or
// $shared; the nearest folder wins) that holds the default auth for
// requests without a `# @auth` directive.
const AuthEnvVar = "$auth"

// Auth is a parsed auth helper:
//
//	basic USER [PASS]
//	bearer TOKEN
//	apikey header|query NAME VALUE
//	digest USER [PASS]
//	none            (no auth, even when the env sets $auth)
type Auth struct {
	Scheme string
	User   string
	Pass   string
	Token  string // bearer token or API key value
	In     string // apikey: "header" or "query"
	Name   string // apikey: header or query parameter name
}

// DigestAuth holds the credentials Send uses to answer a Digest
// challenge. It is never written to JSON.
type DigestAuth struct {
	User string
	Pass string
}

var errAuthSpec = errors.New("@auth: use basic USER PASS, bearer TOKEN, apikey header|query NAME VALUE, digest USER PASS, oauth1 KEY SECRET [TOKEN TOKEN_SECRET] or none")

// ParseAuth reads an auth spec. Errors never echo the values.
func ParseAuth(spec string) (Auth, error) {
	f := splitAuthSpec(spec)
	if len(f) == 0 {
		return Auth{}, errAuthSpec
	}
	rest := func(i int) string { return strings.Join(f[i:], " ") }
	switch s := strings.ToLower(f[0]); s {
	case AuthNone:
		if len(f) == 1 {
			return Auth{Scheme: AuthNone}, nil
		}
	case AuthBasic, AuthDigest:
		if len(f) >= 2 {
			return Auth{Scheme: s, User: f[1], Pass: rest(2)}, nil
		}
	case AuthOAuth1:
		if len(f) == 3 || len(f) == 5 {
			a := Auth{Scheme: AuthOAuth1, User: f[1], Pass: f[2]}
			if len(f) == 5 {
				a.Token, a.Name = f[3], f[4]
			}
			return a, nil
		}
	case AuthBearer:
		if len(f) >= 2 {
			return Auth{Scheme: AuthBearer, Token: rest(1)}, nil
		}
	case AuthAPIKey:
		if len(f) >= 4 {
			in := strings.ToLower(f[1])
			if in == "header" || in == "query" {
				return Auth{Scheme: AuthAPIKey, In: in, Name: f[2], Token: rest(3)}, nil
			}
		}
	}
	return Auth{}, errAuthSpec
}

// splitAuthSpec splits on spaces but keeps a {{…}} placeholder (which
// may hold spaces, like {{$secret pw}}) in one piece.
func splitAuthSpec(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case strings.HasPrefix(s[i:], "{{"):
			depth++
			cur.WriteString("{{")
			i++
			continue
		case depth > 0 && strings.HasPrefix(s[i:], "}}"):
			depth--
			cur.WriteString("}}")
			i++
			continue
		case depth == 0 && (c == ' ' || c == '\t'):
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// authSpec is the request's `# @auth` directive, else the env's $auth.
func (rv *Resolver) authSpec(req Request) (string, bool) {
	if v, ok := req.Directives["auth"]; ok {
		return v, true
	}
	if rv.Envs != nil {
		if v, _, ok := rv.Envs.Lookup(rv.Env, AuthEnvVar); ok && strings.TrimSpace(v) != "" {
			return v, true
		}
	}
	return "", false
}

// applyAuth adds the auth helper to out (already resolved). An explicit
// header of the same name wins. Bad specs and unknown names are
// reported as unresolved in the "auth" field and nothing is added.
func (sc *sendScope) applyAuth(req Request, out *Resolved, un []Unresolved) []Unresolved {
	spec, ok := sc.rv.authSpec(req)
	if !ok {
		return un
	}
	a, err := ParseAuth(spec)
	if err != nil {
		return append(un, Unresolved{Name: "@auth", Field: "auth"})
	}
	bad := false
	exp := func(s string) string {
		v := sc.expand(s)
		if strings.Contains(v, "{{") {
			n := len(un)
			un = appendUnresolved(un, v, "auth")
			bad = bad || len(un) > n
		}
		return v
	}
	a.User, a.Pass, a.Token, a.Name = exp(a.User), exp(a.Pass), exp(a.Token), exp(a.Name)
	if bad {
		return un
	}
	addHeader := func(name, value string) {
		if !out.HasHeader(name) {
			out.Headers = append(slices.Clip(out.Headers), Header{Name: name, Value: value})
		}
	}
	switch a.Scheme {
	case AuthBasic:
		addHeader("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(a.User+":"+a.Pass)))
	case AuthBearer:
		addHeader("Authorization", "Bearer "+a.Token)
	case AuthAPIKey:
		if a.In == "header" {
			addHeader(a.Name, a.Token)
		} else {
			out.URL = addQueryParam(out.URL, a.Name, a.Token)
		}
	case AuthDigest:
		if !out.HasHeader("Authorization") {
			out.Digest = &DigestAuth{User: a.User, Pass: a.Pass}
		}
	case AuthOAuth1:
		// User/Pass: consumer key/secret; Token/Name: token/secret.
		var form url.Values
		if out.Body != nil && strings.HasPrefix(strings.ToLower(out.Header("Content-Type")), "application/x-www-form-urlencoded") {
			form, _ = url.ParseQuery(*out.Body)
		}
		o := OAuth1{ConsumerKey: a.User, ConsumerSecret: a.Pass, Token: a.Token, TokenSecret: a.Name}
		addHeader("Authorization", o.Header(out.Method, out.URL, form, oauth1Nonce(), sc.rv.now()))
	}
	return un
}

// addQueryParam appends name=value to the URL's query (before any
// #fragment), unless the query already has name.
func addQueryParam(u, name, value string) string {
	base, frag, hasFrag := strings.Cut(u, "#")
	if _, q, ok := strings.Cut(base, "?"); ok {
		if vals, err := url.ParseQuery(q); err == nil && vals.Has(name) {
			return u
		}
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
		if strings.HasSuffix(base, "?") || strings.HasSuffix(base, "&") {
			sep = ""
		}
	}
	base += sep + url.QueryEscape(name) + "=" + url.QueryEscape(value)
	if hasFrag {
		base += "#" + frag
	}
	return base
}

// digestTransport answers one Digest challenge (RFC 7616: MD5,
// SHA-256 and their -sess forms, qop auth or none).
type digestTransport struct {
	base http.RoundTripper
	cred DigestAuth
}

func (d *digestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := d.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	var ch map[string]string
	for _, v := range resp.Header.Values("WWW-Authenticate") {
		v = strings.TrimSpace(v)
		if len(v) > 7 && strings.EqualFold(v[:7], "digest ") {
			ch = parseDigestParams(v[7:])
			break
		}
	}
	if ch == nil || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, nil
	}
	h, ok := digestHeader(ch, req.Method, req.URL.RequestURI(), d.cred)
	if !ok {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	r2 := req.Clone(req.Context())
	if req.GetBody != nil {
		if r2.Body, err = req.GetBody(); err != nil {
			return nil, err
		}
	}
	r2.Header.Set("Authorization", h)
	return d.base.RoundTrip(r2)
}

func digestHeader(ch map[string]string, method, uri string, c DigestAuth) (string, bool) {
	algo := ch["algorithm"]
	if algo == "" {
		algo = "MD5"
	}
	var nh func() hash.Hash
	switch strings.ToUpper(strings.TrimSuffix(strings.ToLower(algo), "-sess")) {
	case "MD5":
		nh = md5.New
	case "SHA-256":
		nh = sha256.New
	default:
		return "", false
	}
	hx := func(s string) string {
		h := nh()
		h.Write([]byte(s))
		return hex.EncodeToString(h.Sum(nil))
	}
	realm, nonce := ch["realm"], ch["nonce"]
	if nonce == "" {
		return "", false
	}
	cb := make([]byte, 12)
	_, _ = rand.Read(cb)
	cnonce := hex.EncodeToString(cb)
	ha1 := hx(c.User + ":" + realm + ":" + c.Pass)
	if strings.HasSuffix(strings.ToLower(algo), "-sess") {
		ha1 = hx(ha1 + ":" + nonce + ":" + cnonce)
	}
	ha2 := hx(method + ":" + uri)
	qop := ""
	for _, q := range strings.Split(ch["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}
	const nc = "00000001"
	var resp string
	if qop != "" {
		resp = hx(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		resp = hx(ha1 + ":" + nonce + ":" + ha2)
	}
	q := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	var b strings.Builder
	fmt.Fprintf(&b, "Digest username=%s, realm=%s, nonce=%s, uri=%s, algorithm=%s, response=%s",
		q(c.User), q(realm), q(nonce), q(uri), algo, q(resp))
	if qop != "" {
		fmt.Fprintf(&b, ", qop=%s, nc=%s, cnonce=%s", qop, nc, q(cnonce))
	}
	if op, ok := ch["opaque"]; ok {
		fmt.Fprintf(&b, ", opaque=%s", q(op))
	}
	return b.String(), true
}

// parseDigestParams reads `k=v, k="quoted, \"v\""` pairs (keys lower
// case).
func parseDigestParams(s string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == ',' || s[i] == '\t') {
			i++
		}
		j := i
		for j < len(s) && s[j] != '=' && s[j] != ',' {
			j++
		}
		key := strings.ToLower(strings.TrimSpace(s[i:j]))
		if j >= len(s) || s[j] != '=' {
			i = j + 1
			continue
		}
		j++
		var val strings.Builder
		if j < len(s) && s[j] == '"' {
			j++
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				val.WriteByte(s[j])
				j++
			}
			j++
		} else {
			for j < len(s) && s[j] != ',' {
				val.WriteByte(s[j])
				j++
			}
		}
		if key != "" {
			out[key] = strings.TrimSpace(val.String())
		}
		i = j
	}
	return out
}
