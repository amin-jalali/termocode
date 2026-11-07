package httpclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SigV4Directive is the `# @aws-sigv4 <access key> <secret key>
// <region> <service> [session token]` directive. Its values may hold
// {{vars}} and {{$secret NAME}}; Resolve substitutes them.
const SigV4Directive = "aws-sigv4"

// SigV4Creds are the values of one `# @aws-sigv4` directive.
type SigV4Creds struct {
	AccessKey    string
	SecretKey    string
	Region       string
	Service      string
	SessionToken string
}

var errSigV4Usage = errors.New("aws-sigv4: needs <access key> <secret key> <region> <service> [session token]")

// ParseSigV4 reads a resolved `# @aws-sigv4` value. It fails when a
// field is missing, there are too many, or a placeholder is still raw.
func ParseSigV4(v string) (SigV4Creds, error) {
	f := strings.Fields(v)
	if len(f) < 4 || len(f) > 5 {
		return SigV4Creds{}, errSigV4Usage
	}
	for _, s := range f {
		if strings.Contains(s, "{{") {
			return SigV4Creds{}, errors.New("aws-sigv4: a value is not set (unknown {{variable}})")
		}
	}
	c := SigV4Creds{AccessKey: f[0], SecretKey: f[1], Region: f[2], Service: f[3]}
	if len(f) == 5 {
		c.SessionToken = f[4]
	}
	return c, nil
}

// sigV4Skip are headers that are never signed (they change on the way
// or are set by the signer itself).
var sigV4Skip = map[string]bool{
	"authorization":   true,
	"user-agent":      true,
	"x-amzn-trace-id": true,
	"expect":          true,
	"content-length":  true,
}

// SignSigV4 signs req with AWS Signature Version 4 (header form). It
// sets X-Amz-Date, X-Amz-Security-Token (with a session token),
// X-Amz-Content-Sha256 (for s3) and Authorization. body is the exact
// body that will be sent (nil = none).
func SignSigV4(req *http.Request, body []byte, c SigV4Creds, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])

	req.Header.Del("Authorization")
	req.Header.Set("X-Amz-Date", amzDate)
	if c.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.SessionToken)
	}
	if c.Service == "s3" {
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	}

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{"host": strings.TrimSpace(host)}
	for name, vals := range req.Header {
		n := strings.ToLower(name)
		if sigV4Skip[n] || n == "host" {
			continue
		}
		trimmed := make([]string, len(vals))
		for i, v := range vals {
			trimmed[i] = strings.Join(strings.Fields(v), " ")
		}
		headers[n] = strings.Join(trimmed, ",")
	}
	names := make([]string, 0, len(headers))
	for n := range headers {
		names = append(names, n)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n + ":" + headers[n] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		sigV4Path(req.URL, c.Service != "s3"),
		sigV4Query(req.URL),
		canonHeaders.String(),
		signed,
		payloadHash,
	}, "\n")
	scope := day + "/" + c.Region + "/" + c.Service + "/aws4_request"
	ch := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(ch[:])

	k := hmacSHA256([]byte("AWS4"+c.SecretKey), day)
	k = hmacSHA256(k, c.Region)
	k = hmacSHA256(k, c.Service)
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.AccessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// sigV4Path is the canonical URI: every segment URI-encoded (twice for
// services other than s3, as AWS asks).
func sigV4Path(u *url.URL, double bool) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		raw, err := url.PathUnescape(s)
		if err != nil {
			raw = s
		}
		e := sigV4Escape(raw)
		if double {
			e = sigV4Escape(e)
		}
		segs[i] = e
	}
	return strings.Join(segs, "/")
}

// sigV4Query is the canonical query: keys and values encoded, sorted
// by key then value.
func sigV4Query(u *url.URL) string {
	q := u.Query()
	if len(q) == 0 {
		return ""
	}
	var pairs []string
	for k, vs := range q {
		for _, v := range vs {
			pairs = append(pairs, sigV4Escape(k)+"="+sigV4Escape(v))
		}
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// sigV4Escape is RFC 3986 encoding: only A-Z a-z 0-9 - _ . ~ stay.
func sigV4Escape(s string) string {
	const hexd = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexd[c>>4])
		b.WriteByte(hexd[c&15])
	}
	return b.String()
}
