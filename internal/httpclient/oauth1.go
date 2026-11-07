package httpclient

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OAuth1 signs requests with OAuth 1.0a HMAC-SHA1 (RFC 5849):
// `# @auth oauth1 CONSUMER_KEY CONSUMER_SECRET [TOKEN TOKEN_SECRET]`.
type OAuth1 struct {
	ConsumerKey    string
	ConsumerSecret string
	Token          string
	TokenSecret    string
}

// oauth1Escape is RFC 3986 percent-encoding (only unreserved stay).
func oauth1Escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

func oauth1Nonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Header is the Authorization header for method + rawURL. form holds
// the params of an application/x-www-form-urlencoded body (else nil).
func (a OAuth1) Header(method, rawURL string, form url.Values, nonce string, now time.Time) string {
	oa := map[string]string{
		"oauth_consumer_key":     a.ConsumerKey,
		"oauth_nonce":            nonce,
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        strconv.FormatInt(now.Unix(), 10),
		"oauth_version":          "1.0",
	}
	if a.Token != "" {
		oa["oauth_token"] = a.Token
	}
	type kv struct{ k, v string }
	var params []kv
	base := rawURL
	if u, err := url.Parse(rawURL); err == nil {
		for k, vs := range u.Query() {
			for _, v := range vs {
				params = append(params, kv{oauth1Escape(k), oauth1Escape(v)})
			}
		}
		u.RawQuery, u.Fragment = "", ""
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)
		if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
			u.Host = u.Hostname()
		}
		base = u.String()
	}
	for k, vs := range form {
		for _, v := range vs {
			params = append(params, kv{oauth1Escape(k), oauth1Escape(v)})
		}
	}
	for k, v := range oa {
		params = append(params, kv{oauth1Escape(k), oauth1Escape(v)})
	}
	sort.Slice(params, func(i, j int) bool {
		if params[i].k != params[j].k {
			return params[i].k < params[j].k
		}
		return params[i].v < params[j].v
	})
	parts := make([]string, len(params))
	for i, p := range params {
		parts[i] = p.k + "=" + p.v
	}
	sbs := strings.ToUpper(method) + "&" + oauth1Escape(base) + "&" + oauth1Escape(strings.Join(parts, "&"))
	mac := hmac.New(sha1.New, []byte(oauth1Escape(a.ConsumerSecret)+"&"+oauth1Escape(a.TokenSecret)))
	mac.Write([]byte(sbs))
	oa["oauth_signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	keys := make([]string, 0, len(oa))
	for k := range oa {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := make([]string, len(keys))
	for i, k := range keys {
		h[i] = k + `="` + oauth1Escape(oa[k]) + `"`
	}
	return "OAuth " + strings.Join(h, ", ")
}
