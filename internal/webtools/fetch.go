// Package webtools is the AI's web access (OMX-13): web_fetch reads one
// page as plain text and web_search asks a search provider. Both are off
// until the user turns them on. A fetch only reads (GET), follows at
// most 5 redirects, never reaches a private or loopback address, caps
// the bytes it reads and the text it returns, and blanks known secrets.
package webtools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Limits of one fetch.
const (
	DefaultMaxBytes = 2 << 20 // bytes read from the server
	DefaultMaxText  = 20000   // runes of text returned
	MaxRedirects    = 5
	Timeout         = 20 * time.Second
)

// Options shape a fetch; the zero value is the safe default.
type Options struct {
	MaxBytes int64
	MaxText  int
	// AllowPrivate lets a fetch reach loopback and private addresses
	// (tests with a local fake server only).
	AllowPrivate bool
	// Client (optional) replaces the default client; its transport must
	// do its own address check.
	Client *http.Client
}

// Page is a fetched page as text.
type Page struct {
	URL   string // after redirects
	Type  string // media type
	Title string
	Text  string
	Cut   bool // the page or its text was longer than the limits
}

// ErrBlockedAddress is a fetch to a private, loopback or link-local
// address.
var ErrBlockedAddress = errors.New("private or local addresses are not allowed")

// Client is the HTTP client of a fetch or a search with the address
// check (allowPrivate turns it off).
func Client(allowPrivate bool) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || blockedIP(ip) {
				return ErrBlockedAddress
			}
			return nil
		}
	}
	tr := &http.Transport{DialContext: d.DialContext, Proxy: nil, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: tr, Timeout: Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= MaxRedirects {
			return fmt.Errorf("more than %d redirects", MaxRedirects)
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to %s is not allowed", req.URL.Scheme)
		}
		return nil
	}}
}

// blockedIP: loopback, private, link-local, multicast or unspecified.
func blockedIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64 { // 100.64/10 (CGNAT)
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// CheckURL accepts an absolute http or https URL with a host and no
// user info.
func CheckURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("bad url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("only http and https urls")
	}
	if u.Host == "" {
		return nil, errors.New("the url has no host")
	}
	if u.User != nil {
		return nil, errors.New("urls with a user or password are not allowed")
	}
	return u, nil
}

// Fetch reads rawURL and returns its text.
func Fetch(ctx context.Context, rawURL string, o Options) (Page, error) {
	u, err := CheckURL(rawURL)
	if err != nil {
		return Page{}, err
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.MaxText <= 0 {
		o.MaxText = DefaultMaxText
	}
	c := o.Client
	if c == nil {
		c = Client(o.AllowPrivate)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", "termocode-web-fetch")
	req.Header.Set("Accept", "text/html, text/plain, text/markdown, application/json;q=0.9, */*;q=0.1")
	resp, err := c.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			return Page{}, ErrBlockedAddress
		}
		return Page{}, errors.New(ai.Redact(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, o.MaxBytes+1))
	if err != nil {
		return Page{}, fmt.Errorf("read: %v", err)
	}
	p := Page{URL: resp.Request.URL.String()}
	if int64(len(body)) > o.MaxBytes {
		body, p.Cut = body[:o.MaxBytes], true
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Page{}, fmt.Errorf("HTTP %d from %s", resp.StatusCode, resp.Request.URL.Host)
	}
	p.Type, _, _ = mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if p.Type == "" {
		p.Type = http.DetectContentType(body)
		p.Type, _, _ = mime.ParseMediaType(p.Type)
	}
	switch {
	case p.Type == "text/html" || p.Type == "application/xhtml+xml":
		p.Title, p.Text = HTMLText(string(body))
	case strings.HasPrefix(p.Type, "text/"), p.Type == "application/json", strings.HasSuffix(p.Type, "+json"),
		p.Type == "application/xml", strings.HasSuffix(p.Type, "+xml"):
		p.Text = strings.TrimSpace(string(body))
	default:
		return Page{}, fmt.Errorf("not a text page (%s)", p.Type)
	}
	if r := []rune(p.Text); len(r) > o.MaxText {
		p.Text, p.Cut = string(r[:o.MaxText]), true
	}
	p.Text, p.Title = ai.Redact(p.Text), ai.Redact(p.Title)
	return p, nil
}

var (
	reDrop    = regexp.MustCompile(`(?is)<(script|style|noscript|svg|template|head)\b.*?</(script|style|noscript|svg|template|head)\s*>|<!--.*?-->`)
	reTitle   = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)
	reBreak   = regexp.MustCompile(`(?i)<(br|hr)\b[^>]*>|</(p|div|li|h[1-6]|tr|pre|blockquote|section|article|table|ul|ol|dt|dd)\s*>`)
	reItem    = regexp.MustCompile(`(?i)<li\b[^>]*>`)
	reTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces  = regexp.MustCompile(`[ \t\r\f\v\x{a0}]+`)
	reNewline = regexp.MustCompile(`\n\s*\n+`)
)

// HTMLText turns an HTML page into its title and readable text: scripts,
// styles and comments are dropped, blocks become lines.
func HTMLText(s string) (title, text string) {
	if m := reTitle.FindStringSubmatch(s); m != nil {
		title = strings.TrimSpace(reSpaces.ReplaceAllString(html.UnescapeString(reTag.ReplaceAllString(m[1], "")), " "))
	}
	s = reDrop.ReplaceAllString(s, " ")
	s = reItem.ReplaceAllString(s, "\n- ")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = reSpaces.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	s = reNewline.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return title, strings.TrimSpace(s)
}
