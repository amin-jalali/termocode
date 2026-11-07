package httpclient

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Retry bounds for `# @retry N` and `# @retry-delay D`.
const (
	maxRetries        = 5
	defaultRetryDelay = 500 * time.Millisecond
	maxRetryDelay     = 10 * time.Second
)

// EnvProxyKey is the environment key that sets the proxy for every
// request sent with that environment (a request's `# @proxy` wins).
const EnvProxyKey = "$proxy"

var errBadProxy = errors.New("@proxy: want an http, https or socks5 URL, or none")

// requestProxy reads `# @proxy URL|none`. override is false when the
// send keeps the client's proxy. AI sends (AllowIP set) never use a
// request proxy: dialing a proxy skips the private-address check.
func requestProxy(dirs map[string]string, h Hooks) (func(*http.Request) (*url.URL, error), bool, error) {
	v := strings.TrimSpace(dirs["proxy"])
	if v == "" || h.AllowIP != nil {
		return nil, false, nil
	}
	switch strings.ToLower(v) {
	case "none", "off", "direct":
		return func(*http.Request) (*url.URL, error) { return nil, nil }, true, nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return nil, false, errBadProxy
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, false, errBadProxy
	}
	return http.ProxyURL(u), true, nil
}

// retryLimit reads `# @retry N`: extra tries, 0 when unset or bad.
func retryLimit(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return min(n, maxRetries)
}

// retryDelay is the wait before try+2: base, then twice as long each time.
func retryDelay(base time.Duration, try int) time.Duration {
	d := base
	for range try {
		d *= 2
		if d >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	return min(d, maxRetryDelay)
}

// retryable: network errors, timeouts, 429 and 5xx. Cancel, TLS,
// blocked and redirect errors are final.
func retryable(r *Response) bool {
	switch r.ErrKind {
	case ErrKindDial, ErrKindTimeout:
		return true
	case ErrKindNone:
		return r.Status == http.StatusTooManyRequests || r.Status >= 500
	}
	return false
}
