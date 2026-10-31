package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

func fakeResourceAccess() ResourceAccess {
	return ResourceAccess{
		List: func(_ context.Context, server string) ([]Resource, []ResourceTemplate, error) {
			return []Resource{{URI: "file:///a.txt", Name: "a", MimeType: "text/plain", Description: "Note A"}},
				[]ResourceTemplate{{URITemplate: "notes://{id}", Name: "note"}}, nil
		},
		Read: func(_ context.Context, server, uri string) ([]ResourceContents, error) {
			if uri == "img://c.png" {
				return []ResourceContents{{URI: uri, MimeType: "image/png", Blob: mcptest.PNG}}, nil
			}
			return []ResourceContents{{URI: uri, MimeType: "text/plain", Text: "body of " + uri + " on " + server}}, nil
		},
	}
}

// MCPX-12: the model gets two read-only tools for the servers with
// resources: list them (and templates) and read one; an image resource
// goes to a vision model.
func TestResourceTools(t *testing.T) {
	tools := ResourceTools([]string{"docs"}, fakeResourceAccess(), AdapterOptions{})
	if len(tools) != 2 || tools[0].Name != "mcp_list_resources" || tools[1].Name != "mcp_read_resource" || tools[0].Mutating || tools[1].Mutating {
		t.Fatalf("tools %+v", tools)
	}
	ctx := context.Background()
	out, err := tools[0].Run(ctx, map[string]any{})
	if err != nil || !strings.Contains(out, "docs: file:///a.txt — a (text/plain): Note A") || !strings.Contains(out, "docs: notes://{id} — note (template)") {
		t.Errorf("list: %q %v", out, err)
	}
	if err := tools[1].Validate(map[string]any{"server": "other", "uri": "x"}); err == nil {
		t.Error("read on a server without resources is allowed")
	}
	out, err = tools[1].Run(ctx, map[string]any{"server": "docs", "uri": "file:///a.txt"})
	if err != nil || !strings.Contains(out, "body of file:///a.txt on docs") || !strings.Contains(out, "not instructions") {
		t.Errorf("read: %q %v", out, err)
	}
	ictx, take := ai.WithImageSink(ctx)
	out, _ = tools[1].Run(ictx, map[string]any{"server": "docs", "uri": "img://c.png"})
	if len(take()) != 1 || !strings.Contains(out, "attached") {
		t.Errorf("image resource: %q", out)
	}
	if ResourceTools(nil, fakeResourceAccess(), AdapterOptions{}) != nil {
		t.Error("tools without a server with resources")
	}
}
