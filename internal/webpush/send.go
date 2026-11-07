package webpush

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MaxEndpoint caps an endpoint URL.
const MaxEndpoint = 1024

// DefaultTTL is how long a push service keeps a push for a phone that is
// off: a question that waited longer is better seen on the laptop.
const DefaultTTL = time.Hour

// DefaultHosts are the browser vendors' push services. A subscription
// whose endpoint is elsewhere is refused, so a page cannot make the
// laptop post to any address (no SSRF). An entry that starts with "."
// matches its subdomains.
var DefaultHosts = []string{
	"fcm.googleapis.com",                // Chrome, Edge on Android, Samsung
	"updates.push.services.mozilla.com", // Firefox
	"web.push.apple.com",                // Safari, iOS home-screen apps
	".notify.windows.com",               // Edge on Windows
}

// ErrEndpoint: the endpoint is not a push service's https URL.
var ErrEndpoint = errors.New("webpush: the address is not a known push service")

// CheckEndpoint says whether raw is an https URL of a push service in
// hosts (nil: DefaultHosts) on the default port, with no user info. An
// entry with a port ("127.0.0.1:9999", tests) must match host:port.
func CheckEndpoint(raw string, hosts []string) error {
	if len(raw) == 0 || len(raw) > MaxEndpoint {
		return ErrEndpoint
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Opaque != "" {
		return ErrEndpoint
	}
	if hosts == nil {
		hosts = DefaultHosts
	}
	if slices.Contains(hosts, u.Host) && strings.Contains(u.Host, ":") {
		return nil
	}
	if p := u.Port(); p != "" && p != "443" {
		return ErrEndpoint
	}
	h := strings.ToLower(u.Hostname())
	if net.ParseIP(h) != nil {
		return ErrEndpoint
	}
	for _, e := range hosts {
		if strings.HasPrefix(e, ".") && strings.HasSuffix(h, e) && len(h) > len(e) || h == e {
			return nil
		}
	}
	return ErrEndpoint
}

// Sender posts pushes. Its zero Client is a 10-second client that never
// follows a redirect.
type Sender struct {
	Key *Key
	// Subject is the VAPID contact: a mailto: or https: URL.
	Subject string
	Client  *http.Client
	// Hosts are the allowed push services (nil: DefaultHosts).
	Hosts []string
	// TTL (0: DefaultTTL) and Now (nil: time.Now).
	TTL time.Duration
	Now func() time.Time
}

// noRedirect: a push service answers; it never sends us elsewhere.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

var defaultClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: noRedirect}

// Send posts one push to sub; payload nil is a wake with no body. gone
// says the subscription is dead (404, 410): the caller forgets it. An
// error names the status only, never the endpoint.
func (s *Sender) Send(ctx context.Context, sub Sub, payload []byte) (gone bool, err error) {
	if err := CheckEndpoint(sub.Endpoint, s.Hosts); err != nil {
		return false, err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	var body []byte
	if payload != nil {
		if body, err = Encrypt(sub, payload, rand.Reader); err != nil {
			return false, err
		}
	}
	auth, err := s.Key.Auth(sub.Endpoint, s.Subject, now())
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return false, ErrEndpoint
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("TTL", strconv.Itoa(int(ttl/time.Second)))
	req.Header.Set("Urgency", "high")
	if body != nil {
		req.Header.Set("Content-Encoding", "aes128gcm")
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	c := s.Client
	if c == nil {
		c = defaultClient
	} else if c.CheckRedirect == nil {
		cc := *c
		cc.CheckRedirect = noRedirect
		c = &cc
	}
	res, err := c.Do(req)
	if err != nil {
		return false, errors.New("webpush: the push service did not answer")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	_ = res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return true, nil
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return false, nil
	}
	return false, fmt.Errorf("webpush: the push service said %d", res.StatusCode)
}
