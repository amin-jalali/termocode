package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Resources (MCPX-12, PR-12..14): the client lists a server's resources
// and templates and reads one; notifications/resources/list_changed and
// notifications/resources/updated reach Options.ResourcesChanged.

// ServerCaps are the server features from initialize we use.
type ServerCaps struct {
	Resources   bool
	Subscribe   bool // resources/subscribe
	Prompts     bool
	Completions bool
}

func parseCaps(raw json.RawMessage) ServerCaps {
	var c struct {
		Resources *struct {
			Subscribe bool `json:"subscribe"`
		} `json:"resources"`
		Prompts     json.RawMessage `json:"prompts"`
		Completions json.RawMessage `json:"completions"`
	}
	_ = json.Unmarshal(raw, &c)
	present := func(r json.RawMessage) bool { return len(r) > 0 && string(r) != "null" }
	return ServerCaps{Resources: c.Resources != nil, Subscribe: c.Resources != nil && c.Resources.Subscribe,
		Prompts: present(c.Prompts), Completions: present(c.Completions)}
}

// Capabilities are the server's features (zero before Initialize).
func (c *Client) Capabilities() ServerCaps {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps
}

// Label is the resource's name for people (Resource is in server.go).
func (r Resource) Label() string {
	if r.Title != "" {
		return r.Title
	}
	if r.Name != "" {
		return r.Name
	}
	return r.URI
}

// ResourceTemplate is one entry of resources/templates/list.
type ResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// listPages calls method over its pages and appends each page's key.
func listPages[T any](ctx context.Context, c *Client, method, key string) ([]T, error) {
	var all []T
	cursor := ""
	for page := 0; page < maxToolPages; page++ {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		raw, err := c.call(ctx, method, params)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		var res map[string]json.RawMessage
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("%s: bad answer: %w", method, err)
		}
		var items []T
		if err := json.Unmarshal(res[key], &items); len(res[key]) > 0 && err != nil {
			return nil, fmt.Errorf("%s: bad answer: %w", method, err)
		}
		all = append(all, items...)
		var next string
		_ = json.Unmarshal(res["nextCursor"], &next)
		if next == "" {
			break
		}
		cursor = next
	}
	return all, nil
}

// ListResources returns every resource, following nextCursor.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	return listPages[Resource](ctx, c, "resources/list", "resources")
}

// ListResourceTemplates returns every resource template.
func (c *Client) ListResourceTemplates(ctx context.Context) ([]ResourceTemplate, error) {
	return listPages[ResourceTemplate](ctx, c, "resources/templates/list", "resourceTemplates")
}

// ReadResource reads one resource (text or blob items).
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContents, error) {
	raw, err := c.call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return nil, fmt.Errorf("resources/read: %w", err)
	}
	var res struct {
		Contents []ResourceContents `json:"contents"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("resources/read: bad answer: %w", err)
	}
	return res.Contents, nil
}
