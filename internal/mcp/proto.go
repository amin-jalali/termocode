package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// ProtocolVersion is the MCP revision we offer in initialize. The server
// may answer an older one from SupportedVersions; the stateless
// 2026-07-28 revision is a later step (MC-9).
const ProtocolVersion = "2025-11-25"

// SupportedVersions are the revisions we accept, newest first.
var SupportedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

func supportedVersion(v string) bool {
	for _, s := range SupportedVersions {
		if s == v {
			return true
		}
	}
	return false
}

// ServerInfo is the server's name and version from initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ServerInfo      ServerInfo      `json:"serverInfo"`
	Instructions    string          `json:"instructions"`
}

// Tool is one entry of tools/list.
type Tool struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	InputSchema map[string]any   `json:"inputSchema,omitempty"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
	// Meta is the tool's _meta (maxResultSizeChars, …).
	Meta map[string]any `json:"_meta,omitempty"`
}

// ToolAnnotations are the server's hints about a tool. They are hints,
// not security: the adapter adds its own rules on top (adapter.go).
// Spec defaults when absent: readOnly false, destructive true.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// ReadOnly reports readOnlyHint == true.
func (t Tool) ReadOnly() bool {
	return t.Annotations != nil && t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint
}

// Destructive reports destructiveHint (default true) for a tool that is
// not read-only.
func (t Tool) Destructive() bool {
	if t.ReadOnly() {
		return false
	}
	return t.Annotations == nil || t.Annotations.DestructiveHint == nil || *t.Annotations.DestructiveHint
}

type listToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// Content is one item of a tools/call result.
type Content struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	Data     string            `json:"data,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	URI      string            `json:"uri,omitempty"` // resource_link
	Resource *ResourceContents `json:"resource,omitempty"`
}

// ResourceContents is an embedded resource (text or blob).
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// CallResult is the answer of tools/call.
type CallResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// Text renders a result for the model: text items as they are, binary
// items as a short placeholder, structured content as JSON when there is
// no text, and "error: …" for isError.
func (r CallResult) Text() string {
	var parts []string
	for _, c := range r.Content {
		switch c.Type {
		case "text":
			parts = append(parts, c.Text)
		case "image", "audio":
			parts = append(parts, fmt.Sprintf("[%s %s, %s — not shown]", c.Type, c.MimeType, sizeText(base64.StdEncoding.DecodedLen(len(c.Data)))))
		case "resource":
			if c.Resource == nil {
				continue
			}
			if c.Resource.Text != "" {
				parts = append(parts, c.Resource.Text)
			} else {
				parts = append(parts, fmt.Sprintf("[resource %s, %s]", c.Resource.URI, c.Resource.MimeType))
			}
		case "resource_link":
			parts = append(parts, "[resource "+c.URI+"]")
		}
	}
	out := strings.Join(parts, "\n")
	if strings.TrimSpace(out) == "" && len(r.StructuredContent) > 0 {
		out = string(r.StructuredContent)
	}
	if r.IsError {
		return "error: " + out
	}
	return out
}

func sizeText(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%d KB", n/1024)
}
