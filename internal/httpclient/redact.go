package httpclient

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Redacted replaces a secret value in every text that leaves the
// engine (the model, the audit log, history, toasts, copy-as-curl).
const Redacted = "[REDACTED]"

var secretHeaderNames = map[string]bool{
	"authorization":        true,
	"proxy-authorization":  true,
	"cookie":               true,
	"set-cookie":           true,
	"x-api-key":            true,
	"x-auth-token":         true,
	"x-amz-security-token": true,
}

// SecretHeader reports whether a header's value must be hidden: the
// known auth / cookie names and any name containing token, secret,
// password, api-key / apikey.
func SecretHeader(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if secretHeaderNames[n] {
		return true
	}
	for _, w := range []string{"token", "secret", "password", "passwd", "api-key", "apikey", "api_key", "session"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// RedactHeaderValue hides a secret header's value, keeping what is
// safe to show: the auth scheme ("Bearer [REDACTED]"), cookie names
// and Set-Cookie attributes ("session=[REDACTED]; Path=/; HttpOnly").
func RedactHeaderValue(name, value string) string {
	if !SecretHeader(name) {
		return value
	}
	n := strings.ToLower(name)
	switch n {
	case "authorization", "proxy-authorization":
		if scheme, _, ok := strings.Cut(strings.TrimSpace(value), " "); ok && scheme != "" && !strings.Contains(scheme, "{{") {
			return scheme + " " + Redacted
		}
		return Redacted
	case "cookie":
		parts := strings.Split(value, ";")
		for i, p := range parts {
			k, _, ok := strings.Cut(p, "=")
			if ok {
				parts[i] = k + "=" + Redacted
			} else {
				parts[i] = Redacted
			}
		}
		return strings.Join(parts, ";")
	case "set-cookie":
		first, rest, hasRest := strings.Cut(value, ";")
		k, _, ok := strings.Cut(first, "=")
		out := Redacted
		if ok {
			out = k + "=" + Redacted
		}
		if hasRest {
			out += ";" + rest
		}
		return out
	}
	return Redacted
}

// RedactHeaders returns a copy with every secret value hidden.
func RedactHeaders(hs []Header) []Header {
	if len(hs) == 0 {
		return nil
	}
	out := make([]Header, len(hs))
	for i, h := range hs {
		out[i] = Header{Name: h.Name, Value: RedactHeaderValue(h.Name, h.Value)}
	}
	return out
}

// RedactTemplateHeaders hides secret header values in a request
// template, but keeps values that are only placeholders ("Bearer
// {{token}}"), so a re-send from history resolves them again.
func RedactTemplateHeaders(hs []Header) []Header {
	if len(hs) == 0 {
		return nil
	}
	out := make([]Header, len(hs))
	for i, h := range hs {
		out[i] = h
		if SecretHeader(h.Name) && !onlyPlaceholders(h.Value) {
			out[i].Value = RedactHeaderValue(h.Name, h.Value)
		}
	}
	return out
}

// onlyPlaceholders: v is {{…}} placeholders, optionally after an auth
// scheme word (Bearer, Basic, Token).
func onlyPlaceholders(v string) bool {
	if !strings.Contains(v, "{{") {
		return false
	}
	rest := strings.TrimSpace(placeholderRe.ReplaceAllString(v, ""))
	switch strings.ToLower(rest) {
	case "", "bearer", "basic", "token", "digest":
		return true
	}
	return false
}

// secretQueryRe finds secret-looking query parameters anywhere in a
// text (URLs inside error messages too).
var secretQueryRe = lazyre.New(`(?i)([?&;](?:[\w.-]*(?:token|secret|password|passwd|pwd|api[_-]?key|apikey|signature|sig|auth|session|code|key)[\w.-]*)=)([^&#\s"']+)`)

// userinfoRe finds "scheme://user:password@" in a text; the user may
// be empty ("redis://:password@host").
var userinfoRe = lazyre.New(`(?i)\b([a-z][a-z0-9+.-]*://[^/\s:@]*:)([^/\s@]+)(@)`)

// RedactURL hides secret query values (token, key, password, sig, …)
// and the password part of user:password@ in s. It works on any text,
// not only on a bare URL.
func RedactURL(s string) string {
	if s == "" {
		return s
	}
	s = secretQueryRe.ReplaceAllString(s, "${1}"+Redacted)
	s = userinfoRe.ReplaceAllString(s, "${1}"+Redacted+"${3}")
	return s
}

// HostClass is how the AI gate sees a host.
type HostClass int

// Host classes.
const (
	HostPublic HostClass = iota
	// HostLoopback: localhost, 127.0.0.0/8, ::1, 0.0.0.0.
	HostLoopback
	// HostPrivate: RFC 1918, link-local (cloud metadata), fc00::/7,
	// CGNAT, *.internal, *.local, bare names without a dot.
	HostPrivate
)

// ClassifyHost classifies a host name or IP (no DNS lookup).
func ClassifyHost(host string) HostClass {
	h := strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return HostLoopback
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return HostLoopback
		}
		if IsPrivateIP(ip) {
			return HostPrivate
		}
		return HostPublic
	}
	// System resolvers still read short, octal and hex IPv4 forms
	// ("127.1", "0x7f.0.0.1"); class them by the address they name.
	if ip, ok := looseIPv4(h); ok {
		if c := ClassifyHost(ip.String()); c != HostPublic {
			return c
		}
	}
	if !strings.Contains(h, ".") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".local") ||
		strings.HasSuffix(h, ".lan") || strings.HasSuffix(h, ".home.arpa") {
		return HostPrivate
	}
	return HostPublic
}

// looseIPv4 reads an IPv4 address the way inet_aton does: one to four
// dot parts, each decimal, octal (leading 0) or hex (0x); the last part
// fills the bytes that are left.
func looseIPv4(h string) (net.IP, bool) {
	parts := strings.Split(h, ".")
	if len(parts) > 4 {
		return nil, false
	}
	var vals []uint64
	for _, p := range parts {
		base, digits := 10, p
		switch {
		case strings.HasPrefix(p, "0x"):
			base, digits = 16, p[2:]
		case len(p) > 1 && p[0] == '0':
			base, digits = 8, p[1:]
		}
		if digits == "" && base != 8 {
			return nil, false
		}
		if digits == "" {
			digits = "0"
		}
		v, err := strconv.ParseUint(digits, base, 32)
		if err != nil {
			return nil, false
		}
		vals = append(vals, v)
	}
	var n uint64
	for i, v := range vals[:len(vals)-1] {
		if v > 0xff {
			return nil, false
		}
		n |= v << (24 - 8*i)
	}
	last := vals[len(vals)-1]
	if last >= 1<<(8*(5-len(vals))) {
		return nil, false
	}
	n |= last
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)), true
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// IsPrivateIP reports a private or special address that is not
// loopback: RFC 1918, link-local (169.254/16, cloud metadata, fe80::),
// unique local fc00::/7, CGNAT 100.64/10, multicast.
func IsPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return false
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip)
}

// HostKey is "host" or "host:port" from a URL, lower-cased; the AI
// gate's "Always allow" class uses it so a different port is a
// different service.
func HostKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}
