package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Client talks to GitHub. Every address must be https, redirects too.
type Client struct {
	HTTP      *http.Client // nil: a client with a 30 s timeout per request
	LatestURL string       // "" = LatestURL
	Version   string       // the running version, for the User-Agent
}

// errNotHTTPS refuses a plain-http address.
var errNotHTTPS = errors.New("update: only https addresses are used")

func requireHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return errNotHTTPS
	}
	return nil
}

// httpClient is c.HTTP with a redirect check that keeps https.
func (c Client) httpClient() *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{Timeout: 30 * time.Second}
	}
	cp := *base
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return errNotHTTPS
		}
		return nil
	}
	return &cp
}

func (c Client) get(ctx context.Context, raw string, accept string) (*http.Response, error) {
	if err := requireHTTPS(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	ua := "termocode/" + c.Version
	if c.Version == "" {
		ua = "termocode/dev"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %d", raw, resp.StatusCode)
	}
	return resp, nil
}

// Latest asks GitHub for the newest full release (no token).
func (c Client) Latest(ctx context.Context) (Release, error) {
	u := c.LatestURL
	if u == "" {
		u = LatestURL
	}
	resp, err := c.get(ctx, u, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	return ParseRelease(resp.Body)
}

// Download saves raw into the new file dest, at most max bytes. On error
// dest does not exist.
func (c Client) Download(ctx context.Context, raw, dest string, max int64) error {
	resp, err := c.get(ctx, raw, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > max {
		return fmt.Errorf("%s is too large (%d bytes)", raw, resp.ContentLength)
	}
	return writeLimited(dest, resp.Body, max)
}
