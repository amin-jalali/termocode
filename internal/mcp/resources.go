package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
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

// withClient runs fn on a server's connection, starting it (with the
// trust question) when needed; the idle timer starts again after.
func (m *Manager) withClient(ctx context.Context, ask AskTrust, server string, fn func(context.Context, *Client) error) error {
	st, err := m.get(server)
	if err != nil {
		return err
	}
	st.op.Lock()
	defer st.op.Unlock()
	p, err := m.ensure(ctx, st, ask)
	if err != nil {
		return err
	}
	m.mu.Lock()
	st.caps = p.Client.Capabilities()
	m.mu.Unlock()
	defer m.armIdle(st)
	cctx, cancel := context.WithTimeout(ctx, m.callTimeout(st))
	defer cancel()
	return fn(cctx, p.Client)
}

func (m *Manager) callTimeout(st *serverState) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := st.cfg.Timeout; t > 0 {
		return time.Duration(t) * time.Second
	}
	return m.opts.CallTimeout
}

// Resources lists a server's resources and templates (MCPX-12). A
// server without the resources feature has none.
func (m *Manager) Resources(ctx context.Context, ask AskTrust, server string) ([]Resource, []ResourceTemplate, error) {
	var rs []Resource
	var ts []ResourceTemplate
	err := m.withClient(ctx, ask, server, func(ctx context.Context, c *Client) error {
		if !c.Capabilities().Resources {
			return nil
		}
		var err error
		if rs, err = c.ListResources(ctx); err != nil {
			return err
		}
		ts, _ = c.ListResourceTemplates(ctx) // optional: many servers have none
		return nil
	})
	return rs, ts, err
}

// ReadResource reads one resource of a server.
func (m *Manager) ReadResource(ctx context.Context, ask AskTrust, server, uri string) ([]ResourceContents, error) {
	var out []ResourceContents
	err := m.withClient(ctx, ask, server, func(ctx context.Context, c *Client) error {
		if !c.Capabilities().Resources {
			return fmt.Errorf("MCP server %s has no resources", server)
		}
		var err error
		out, err = c.ReadResource(ctx, uri)
		return err
	})
	return out, err
}
