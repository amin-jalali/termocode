package mcptest

import (
	"encoding/json"
	"strings"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// CodeResourceNotFound is the spec's error for an unknown resource.
const CodeResourceNotFound = -32002

// PNG is a tiny real PNG (1x1), base64.
const PNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// Resources answers the fake's resources/* requests.
func Resources(method string, params json.RawMessage) (any, *extproc.Error) {
	var p struct {
		Cursor string `json:"cursor"`
		URI    string `json:"uri"`
	}
	_ = json.Unmarshal(params, &p)
	switch method {
	case "resources/list":
		if p.Cursor == "" {
			return map[string]any{"resources": []any{
				map[string]any{"uri": "file:///notes/a.txt", "name": "a", "title": "Note A", "mimeType": "text/plain"},
				map[string]any{"uri": "file:///notes/b.md", "name": "b", "mimeType": "text/markdown"},
			}, "nextCursor": "r2"}, nil
		}
		return map[string]any{"resources": []any{
			map[string]any{"uri": "img://chart.png", "name": "chart", "mimeType": "image/png"},
		}}, nil
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": []any{
			map[string]any{"uriTemplate": "notes://{id}", "name": "note", "description": "A note by id"},
		}}, nil
	}
	one := func(c map[string]any) (any, *extproc.Error) {
		c["uri"] = p.URI
		return map[string]any{"contents": []any{c}}, nil
	}
	switch {
	case p.URI == "file:///notes/a.txt":
		return one(map[string]any{"mimeType": "text/plain", "text": "note a body"})
	case p.URI == "file:///notes/b.md":
		return one(map[string]any{"mimeType": "text/markdown", "text": "# B"})
	case p.URI == "img://chart.png":
		return one(map[string]any{"mimeType": "image/png", "blob": PNG})
	case strings.HasPrefix(p.URI, "notes://"):
		return one(map[string]any{"mimeType": "text/plain", "text": "note " + strings.TrimPrefix(p.URI, "notes://")})
	}
	return nil, &extproc.Error{Code: CodeResourceNotFound, Message: "resource not found: " + p.URI}
}
