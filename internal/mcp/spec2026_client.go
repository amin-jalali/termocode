package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// The stateless 2026-07-28 revision on the client side (MCPX-20).
// Initialize first probes with server/discover. A server that lists
// StatelessVersion is used statelessly: no initialize, and every request
// carries the revision, termocode's name and its capabilities in _meta.
// Any other answer (an error, a 4xx, no answer in time) means an older
// server, and the initialize handshake runs as before.

// metaCapsKey is the _meta key of the client's capabilities.
const metaCapsKey = "io.modelcontextprotocol/clientCapabilities"

// CodeUnsupportedVersion is UnsupportedProtocolVersionError.
const CodeUnsupportedVersion = -32022

// discoverTimeout caps the server/discover probe: an old server that
// stays silent on it must not cost the whole start.
var discoverTimeout = 3 * time.Second

func clientInfo() map[string]any { return map[string]any{"name": "termocode", "version": "1"} }

func clientCapabilities() map[string]any {
	return map[string]any{"roots": map[string]any{"listChanged": false},
		"elicitation": map[string]any{"form": map[string]any{}, "url": map[string]any{}}}
}

type discoverResult struct {
	SupportedVersions []string        `json:"supportedVersions"`
	Capabilities      json.RawMessage `json:"capabilities"`
	ServerInfo        ServerInfo      `json:"serverInfo"` // early drafts
	Meta              struct {
		ServerInfo *ServerInfo `json:"io.modelcontextprotocol/serverInfo"`
	} `json:"_meta"`
}

// Probed reports that Initialize asked server/discover.
func (c *Client) Probed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.probed
}

// Stateless reports a connection in the stateless revision.
func (c *Client) Stateless() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateless
}

// discover probes the server. modern is true when the client now speaks
// the stateless revision; err is a failure that is not "an old server".
func (c *Client) discover(ctx context.Context) (modern bool, err error) {
	pv, _ := c.conn.(interface{ setProtocolVersion(string) })
	if pv != nil {
		pv.setProtocolVersion(StatelessVersion)
	}
	pctx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()
	raw, err := c.conn.Call(pctx, "server/discover", withMeta(nil))
	var res discoverResult
	switch {
	case err == nil && json.Unmarshal(raw, &res) == nil && contains(res.SupportedVersions, StatelessVersion):
	case err != nil && ctx.Err() != nil:
		return false, ctx.Err()
	case err != nil && !oldServerError(err) && pctx.Err() == nil:
		if errors.Is(err, extproc.ErrClosed) {
			return false, errors.New("the MCP server closed the connection")
		}
		return false, err
	default:
		if pv != nil {
			pv.setProtocolVersion("")
		}
		return false, nil
	}
	info := res.ServerInfo
	if res.Meta.ServerInfo != nil {
		info = *res.Meta.ServerInfo
	}
	c.mu.Lock()
	c.stateless, c.version, c.info, c.caps = true, StatelessVersion, info, parseCaps(res.Capabilities)
	c.mu.Unlock()
	c.opts.log("speaks MCP %s (stateless)", StatelessVersion)
	return true, nil
}

// oldServerError is an answer to the probe that means "an older server":
// a JSON-RPC error or an HTTP 4xx that is not about sign-in.
func oldServerError(err error) bool {
	var re *extproc.Error
	if errors.As(err, &re) {
		return true
	}
	var se *StatusError
	return errors.As(err, &se) && se.Code >= 400 && se.Code < 500
}

// withMeta returns params with the stateless _meta added (a copy; the
// caller's own _meta keys stay).
func withMeta(params any) map[string]any {
	m := map[string]any{}
	switch p := params.(type) {
	case nil:
	case map[string]any:
		for k, v := range p {
			m[k] = v
		}
	default:
		if b, err := json.Marshal(p); err == nil {
			_ = json.Unmarshal(b, &m)
		}
	}
	meta := map[string]any{}
	if old, ok := m["_meta"].(map[string]any); ok {
		for k, v := range old {
			meta[k] = v
		}
	}
	meta[metaVersionKey] = StatelessVersion
	meta[metaClientKey] = clientInfo()
	caps := clientCapabilities()
	caps["extensions"] = map[string]any{tasksExtension: map[string]any{}} // spec2026_tasks.go
	meta[metaCapsKey] = caps
	m["_meta"] = meta
	return m
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
