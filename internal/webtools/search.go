package webtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Provider is a web search service.
type Provider struct {
	Name     string // the setting value ("brave")
	Title    string // "Brave Search"
	Endpoint string
	KeyPage  string // where a key is made
	EnvKey   string // the environment variable that may hold the key
}

// Providers are the search services web_search can use.
var Providers = []Provider{
	{Name: "brave", Title: "Brave Search", Endpoint: "https://api.search.brave.com/res/v1/web/search",
		KeyPage: "https://api-dashboard.search.brave.com/app/keys", EnvKey: "BRAVE_API_KEY"},
	{Name: "tavily", Title: "Tavily", Endpoint: "https://api.tavily.com/search",
		KeyPage: "https://app.tavily.com/home", EnvKey: "TAVILY_API_KEY"},
	{Name: "exa", Title: "Exa", Endpoint: "https://api.exa.ai/search",
		KeyPage: "https://dashboard.exa.ai/api-keys", EnvKey: "EXA_API_KEY"},
}

// ProviderByName finds a provider.
func ProviderByName(name string) (Provider, bool) {
	for _, p := range Providers {
		if p.Name == name {
			return p, true
		}
	}
	return Provider{}, false
}

// Result is one search hit.
type Result struct {
	Title, URL, Snippet string
}

// MaxResults caps one search.
const MaxResults = 10

// Search asks p for query (n results, 1…MaxResults). The key goes only
// in the provider's header; it never appears in an error.
func Search(ctx context.Context, c *http.Client, p Provider, key, query string, n int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("empty query")
	}
	if key == "" {
		return nil, fmt.Errorf("no %s key", p.Title)
	}
	n = max(1, min(n, MaxResults))
	var req *http.Request
	var err error
	switch p.Name {
	case "brave":
		u := p.Endpoint + "?" + url.Values{"q": {query}, "count": {strconv.Itoa(n)}}.Encode()
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err == nil {
			req.Header.Set("X-Subscription-Token", key)
		}
	case "tavily":
		req, err = jsonRequest(ctx, p.Endpoint, map[string]any{"query": query, "max_results": n})
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	case "exa":
		req, err = jsonRequest(ctx, p.Endpoint, map[string]any{"query": query, "numResults": n,
			"contents": map[string]any{"text": map[string]any{"maxCharacters": 300}}})
		if err == nil {
			req.Header.Set("x-api-key", key)
		}
	default:
		return nil, fmt.Errorf("unknown search provider %q", p.Name)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, errors.New(redactKey(ai.Redact(err.Error()), key))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, DefaultMaxBytes))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%s said HTTP %d: check the key", p.Title, resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, fmt.Errorf("%s said HTTP %d", p.Title, resp.StatusCode)
	}
	var doc struct {
		Web struct {
			Results []struct{ Title, URL, Description string }
		}
		Results []struct{ Title, URL, Content, Text string }
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("%s: bad answer", p.Title)
	}
	var out []Result
	for _, r := range doc.Web.Results {
		out = append(out, Result{Title: r.Title, URL: r.URL, Snippet: r.Description})
	}
	for _, r := range doc.Results {
		s := r.Content
		if s == "" {
			s = r.Text
		}
		out = append(out, Result{Title: r.Title, URL: r.URL, Snippet: s})
	}
	if len(out) > n {
		out = out[:n]
	}
	for i := range out {
		r := &out[i]
		_, r.Snippet = HTMLText(r.Snippet) // some providers mark hits with <strong>
		r.Title, r.Snippet = ai.Redact(strings.TrimSpace(r.Title)), redactKey(ai.Redact(r.Snippet), key)
		if rr := []rune(r.Snippet); len(rr) > 300 {
			r.Snippet = string(rr[:299]) + "…"
		}
	}
	return out, nil
}

func jsonRequest(ctx context.Context, endpoint string, body any) (*http.Request, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, err
}

func redactKey(s, key string) string {
	if len(key) < 6 {
		return s
	}
	return strings.ReplaceAll(s, key, "•••")
}
