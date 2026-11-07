package devpreview

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Check is what one probe of a target saw.
type Check struct {
	Up     bool   // something answered HTTP
	Status int    // the HTTP status (0: no answer)
	Title  string // the page <title>, trimmed (may be "")
	Err    string // why it did not answer (Up false)
}

// Hint is the short text a list row shows: "200 · My App", "not
// answering".
func (c Check) Hint() string {
	if !c.Up {
		if c.Err != "" {
			return "not answering (" + c.Err + ")"
		}
		return "not answering"
	}
	s := fmt.Sprint(c.Status)
	if c.Title != "" {
		s += " · " + c.Title
	}
	return s
}

// ProbeTimeout bounds one probe.
const ProbeTimeout = 2 * time.Second

// maxBody is how much of the page a probe reads to find the title.
const maxBody = 64 << 10

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// errNotLocal: a probe or redirect left loopback.
var errNotLocal = errors.New("not a local address")

// loopbackDial refuses any address that is not a loopback IP after
// resolving, so a "localhost" mapped elsewhere is not reached.
func loopbackDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	for _, ip := range ips {
		if ip.IP.IsLoopback() {
			return d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		}
	}
	return nil, errNotLocal
}

// client is the probe's HTTP client: no environment proxy, loopback
// dials only, at most 3 redirects and none away from loopback. A dev
// server's self-signed certificate is accepted: only loopback is ever
// dialed and only the status and title are read.
var client = &http.Client{
	Timeout: ProbeTimeout,
	Transport: &http.Transport{
		Proxy:             nil,
		DialContext:       loopbackDial,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // loopback only, read only
		DisableKeepAlives: true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		if !IsLoopbackHost(req.URL.Hostname()) {
			return errNotLocal
		}
		return nil
	},
}

// Probe asks rawURL once. Anything that is not a loopback http(s)
// address is refused without a request.
func Probe(ctx context.Context, rawURL string) Check {
	u, _, ok := Normalize(rawURL)
	if !ok {
		return Check{Err: errNotLocal.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Check{Err: err.Error()}
	}
	req.Header.Set("Accept", "text/html,*/*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return Check{Err: shortErr(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	c := Check{Up: true, Status: resp.StatusCode}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		c.Title = PageTitle(b)
	}
	return c
}

// PageTitle is the first <title> of an HTML page, unescaped, with runs
// of white space folded and at most 60 characters.
func PageTitle(page []byte) string {
	m := titleRe.FindSubmatch(page)
	if m == nil {
		return ""
	}
	t := strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " ")
	if utf8.RuneCountInString(t) > 60 {
		r := []rune(t)
		t = string(r[:59]) + "…"
	}
	return t
}

// shortErr is a probe error in a few words: "refused", "timeout".
func shortErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	s := err.Error()
	switch {
	case errors.Is(err, errNotLocal) || strings.Contains(s, errNotLocal.Error()):
		return "redirects away from this machine"
	case strings.Contains(s, "connection refused"):
		return "refused"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		return "timeout"
	}
	return s
}

// ProbeAll probes the targets at the same time and returns the checks
// in the same order.
func ProbeAll(ctx context.Context, targets []Target) []Check {
	out := make([]Check, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		u := t.URL
		crash.Go("devpreview.ProbeAll", func() {
			defer wg.Done()
			out[i] = Probe(ctx, u)
		})
	}
	wg.Wait()
	return out
}
