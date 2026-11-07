package assets

import (
	"regexp"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (MOBUX-14): the phone's MCP default views use the laptop's op
// names, mark every layout with a shape, keep the raw JSON one tap away,
// offer "Ask the AI" for a tool that may not run, and stay out of the
// first files (a lazy phone chunk loads them).
func TestMCPViewsPage(t *testing.T) {
	files, _ := loadSources(t)
	view, ops, str := files["src/phone/mcp/McpViews.tsx"], files["src/phone/mcp/mcpops.ts"], files["src/phone/mcp/strings.ts"]
	if view == "" || ops == "" || str == "" {
		t.Fatal("the MCP views files are missing")
	}
	for _, want := range []string{`OP_VIEWS = "` + webproto.OpMCPViews + `"`, `OP_RUN = "` + webproto.OpMCPViewRun + `"`} {
		if !strings.Contains(ops, want) {
			t.Errorf("mcpops.ts lacks %s", want)
		}
	}
	for _, layout := range []string{mcp.LayoutTable, mcp.LayoutList, mcp.LayoutCard, mcp.LayoutDetail, mcp.LayoutText, mcp.LayoutJSON, mcp.LayoutStatus} {
		if !regexp.MustCompile(`\b` + layout + `: \{ mark: "[^"]+"`).MatchString(ops) {
			t.Errorf("layout %s has no shape mark", layout)
		}
	}
	for _, want := range []string{`"mcpv.raw"`, `"mcpv.ask_ai"`, "<BlockView", "res.raw", "onAsk(askText)"} {
		if !strings.Contains(view, want) {
			t.Errorf("McpViews.tsx lacks %s", want)
		}
	}
	for _, bad := range []string{"innerHTML", "dangerouslySetInnerHTML", "eval(", "new Function", `from "../../session"`} {
		if strings.Contains(view, bad) || strings.Contains(ops, bad) {
			t.Errorf("the MCP views use %s", bad)
		}
	}
	if regexp.MustCompile(`[\x{0600}-\x{06FF}]`).MatchString(str + view) {
		t.Error("the phone app is English only")
	}
	for _, entry := range []string{"src/main.tsx", "src/workbench.tsx"} {
		if reachable(files, entry)["src/phone/mcp/McpViews.tsx"] {
			t.Errorf("%s reaches McpViews.tsx; it belongs in a lazy phone chunk", entry)
		}
	}
}
