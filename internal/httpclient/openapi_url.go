package httpclient

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// IsOpenAPIURL reports whether s is an http(s) URL (HTTP: Import
// OpenAPI… takes a file path or a URL).
func IsOpenAPIURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// FetchOpenAPI downloads a spec with c (GET, at most max bytes). It
// fails on a send error, a status that is not 2xx or a bigger body.
func FetchOpenAPI(ctx context.Context, c *Client, specURL string, max int64) ([]byte, error) {
	if !IsOpenAPIURL(specURL) {
		return nil, fmt.Errorf("not an http or https URL: %q", specURL)
	}
	rr := Resolved{Request: Request{Method: "GET", URL: strings.TrimSpace(specURL), Headers: []Header{
		{Name: "Accept", Value: "application/json, application/yaml, text/yaml, */*"},
	}}}
	resp := c.Send(ctx, rr, Hooks{})
	switch {
	case resp.Err != "":
		return nil, fmt.Errorf("%s", resp.Err)
	case resp.Status < 200 || resp.Status > 299:
		return nil, fmt.Errorf("the server answered %s", resp.StatusLine())
	case resp.Truncated || int64(len(resp.Body)) > max:
		return nil, fmt.Errorf("the spec is larger than %s", FormatSize(max))
	}
	return resp.Body, nil
}
