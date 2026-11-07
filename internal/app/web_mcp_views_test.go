package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// MOBUX-14: the shaped data reaches the phone as one valid block, the
// raw JSON beside it within its cap and never cut inside a rune.
func TestWebMCPFillCaps(t *testing.T) {
	rows := make([][]mcp.Cell, 250)
	for i := range rows {
		rows[i] = []mcp.Cell{{S: strings.Repeat("a", 190)}, {S: "line\nbreak"}}
	}
	d := mcp.ViewData{Layout: mcp.LayoutTable, Fields: []mcp.ViewField{{ID: "a b", Label: "A", Kind: mcp.FieldText}, {ID: "n", Label: "N", Kind: mcp.FieldText}},
		Rows: rows, Raw: strings.Repeat("é", webproto.MaxMCPViewRaw)}
	out := webproto.MCPViewData{View: "mcp.s.@resources", Title: "S"}
	webMCPFill(&out, d)
	if out.Block == nil || out.Block.Validate() != nil {
		t.Fatalf("block does not pass the caps: %v", out.Block.Validate())
	}
	if out.Block.Kind != "table" || !out.Truncated || len(out.Block.Rows) == 0 {
		t.Fatalf("kind %q rows %d truncated %v", out.Block.Kind, len(out.Block.Rows), out.Truncated)
	}
	if len(out.Raw) > webproto.MaxMCPViewRaw || !strings.HasSuffix(out.Raw, "é") {
		t.Fatalf("raw %d bytes or cut inside a rune", len(out.Raw))
	}
}

func TestWebMCPViewInfo(t *testing.T) {
	v := mcp.ToolView{ID: "mcp.gh.list", Tool: "list", Title: "List", Layout: mcp.LayoutTable, Run: true,
		Params: []mcp.ViewParam{{ID: "repo", Label: "Repo", Kind: "string", Required: true}},
		Fields: []mcp.ViewField{{ID: "n", Label: "N", Kind: mcp.FieldNum}}}
	info := webMCPViewInfo(v)
	if info.ID != "mcp.gh.list" || !info.Run || len(info.Params) != 1 || len(info.Fields) != 1 || info.Layout != "table" {
		t.Fatalf("%+v", info)
	}
	if webMCPTrustWhy(mcp.Status{ToolsChanged: true}) == webMCPTrustWhy(mcp.Status{}) {
		t.Fatal("changed tools and an untrusted server say the same")
	}
	if a := webMCPPhoneAsk(context.TODO(), mcp.Server{}); a != mcp.TrustDeny {
		t.Fatal("the phone may trust a server")
	}
}
